// Package session 是 termhub 的核心接缝：SessionManager。
// 进程管理、终端流的事件桥推送、历史读写后续都会从这里发起；
// 前端与单测都只依赖本包暴露的 Service 结构。
package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shihao-hub/termhub/internal/pty"
)

// Status 会话状态。
type Status string

const (
	StatusRunning Status = "running"
	StatusExited  Status = "exited"
)

// Session 一个终端会话的全部运行时状态。
type Session struct {
	ID        string
	Status    Status
	StartedAt time.Time
	ExitedAt  time.Time
	ExitCode  int
	// Source 会话来源：manual / 启动项 id（T02 起使用）。
	Source string
	// WorkDir 会话初始工作目录，窗口重建时用于恢复 tab。
	WorkDir string

	mu sync.Mutex
	// pty 为 nil 时不允许写输入（会话已退出）。
	pt      pty.Term
	cols    int
	rows    int
	buffer  []byte // 最近输出环形截存，供新接入的 WS 客户端回放一屏
	subs    map[chan []byte]struct{}
	stopped chan struct{} // 关闭即表示会话已退出
}

// Service 终端会话管理器（测试接缝）。
type Service struct {
	Factory     pty.Factory
	CommandLine string // 默认 shell 命令行（T01 固定 PowerShell）

	mu       sync.Mutex
	sessions map[string]*Session
	seq      int
}

// NewService 构造会话管理器；factory 由调用方注入（真 ConPTY 或 fake）。
func NewService(factory pty.Factory) *Service {
	return &Service{
		Factory:     factory,
		CommandLine: "powershell.exe -NoLogo",
		sessions:    make(map[string]*Session),
	}
}

// StartSession 拉起一个终端会话，返回会话 ID。
func (s *Service) StartSession(ctx context.Context, opts pty.Options) (string, error) {
	cols, rows := opts.Cols, opts.Rows
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 30
	}
	opts.Cols, opts.Rows = cols, rows
	if opts.CommandLine == "" {
		opts.CommandLine = "powershell.exe -NoLogo"
	}

	t, err := s.Factory.Start(opts)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.seq++
	id := fmt.Sprintf("s%03d", s.seq)
	sess := &Session{
		ID:        id,
		Status:    StatusRunning,
		StartedAt: time.Now(),
		Source:    opts.Source,
		WorkDir:   opts.WorkDir,
		pt:        t,
		cols:      cols,
		rows:      rows,
		subs:      make(map[chan []byte]struct{}),
		stopped:   make(chan struct{}),
	}
	s.sessions[id] = sess
	s.mu.Unlock()

	go s.pump(sess)
	return id, nil
}

// pump 从 PTY 读输出：推给所有订阅者并保持最近一屏缓冲。
func (s *Service) pump(sess *Session) {
	buf := make([]byte, 4096)
	for {
		n, err := sess.pt.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			sess.mu.Lock()
			sess.buffer = append(sess.buffer, chunk...)
			if len(sess.buffer) > maxBuffer {
				sess.buffer = sess.buffer[len(sess.buffer)-maxBuffer:]
			}
			for ch := range sess.subs {
				select {
				case ch <- chunk:
				default: // 慢客户端丢弃本次推流，避免阻塞其他订阅者
				}
			}
			sess.mu.Unlock()
		}
		if err != nil {
			s.finish(sess, 0)
			return
		}
	}
}

// finish 会话收尾：更新状态并广播退出事件。
func (s *Service) finish(sess *Session, code int) {
	sess.mu.Lock()
	if sess.Status == StatusExited {
		sess.mu.Unlock()
		return
	}
	sess.Status = StatusExited
	sess.ExitedAt = time.Now()
	sess.ExitCode = code
	sess.pt = nil
	sess.mu.Unlock()
	close(sess.stopped)

	s.mu.Lock()
	delete(s.sessions, sess.ID)
	s.mu.Unlock()
}

// Subscribe 订阅某会话的输出流；返回最近缓冲（回放）、实时订阅通道与退路通知。
func (s *Service) Subscribe(id string) ([]byte, <-chan []byte, <-chan struct{}, error) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return nil, nil, nil, fmt.Errorf("会话不存在: %s", id)
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	backlog := append([]byte(nil), sess.buffer...)
	ch := make(chan []byte, 64)
	sess.subs[ch] = struct{}{}
	return backlog, ch, sess.stopped, nil
}

// Unsubscribe 取消订阅（WS 断开时调用）。ch 必须是 Subscribe 返回的同一通道。
func (s *Service) Unsubscribe(id string, ch chan []byte) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return
	}
	sess.mu.Lock()
	delete(sess.subs, ch)
	sess.mu.Unlock()
}

// WriteInput 把用户键入写到某会话的 PTY。
func (s *Service) WriteInput(id string, data []byte) error {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("会话不存在: %s", id)
	}
	sess.mu.Lock()
	pt := sess.pt
	sess.mu.Unlock()
	if pt == nil {
		return fmt.Errorf("会话已退出: %s", id)
	}
	_, err := pt.Write(data)
	return err
}

// Resize 更新某会话的视口尺寸。
func (s *Service) Resize(id string, cols, rows int) error {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("会话不存在: %s", id)
	}
	sess.mu.Lock()
	pt := sess.pt
	sess.mu.Unlock()
	if pt == nil {
		return fmt.Errorf("会话已退出: %s", id)
	}
	sess.cols, sess.rows = cols, rows
	return pt.Resize(cols, rows)
}

// Snapshot 会话的对外视图。
type Snapshot struct {
	ID        string      `json:"id"`
	Status    Status      `json:"status"`
	StartedAt time.Time   `json:"started_at"`
	ExitCode  int         `json:"exit_code"`
	WorkDir   string      `json:"work_dir"`
	Opts      pty.Options `json:"-"`
}

// ListSessions 返回全部会话快照（T04 面板的数据源雏形）。
func (s *Service) ListSessions() []Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Snapshot, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, Snapshot{
			ID:        sess.ID,
			Status:    sess.Status,
			StartedAt: sess.StartedAt,
			ExitCode:  sess.ExitCode,
			WorkDir:   sess.WorkDir,
		})
	}
	return out
}

// Shutdown 关闭全部会话的 PTY，供应用退出时回收整棵进程树。
func (s *Service) Shutdown() {
	s.mu.Lock()
	sessions := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()

	for _, sess := range sessions {
		sess.mu.Lock()
		pt := sess.pt
		sess.mu.Unlock()
		if pt != nil {
			_ = pt.Close()
		}
	}
}

// maxBuffer 每会话保留的最多输出字节（约十几屏）。
const maxBuffer = 256 * 1024
