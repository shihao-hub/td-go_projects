// Package terminal 提供 ConPTY 终端会话桥接：拉起外部 typeai.exe，
// 转发双向字节流、同步窗口尺寸并通知退出。本包不依赖 Wails，可独立测试。
package terminal

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"
)

// 默认终端尺寸（列 x 行），与 ConPTY 创建基线一致。
const (
	defaultCols = 80
	defaultRows = 25
)

// 收尾时序常量（Windows ConPTY 的两个硬约束决定，均有实验依据）：
//   - 子进程退出后 conhost 不会关闭输出管道（Read 永不 EOF），必须主动
//     ClosePseudoConsole 才能唤醒读循环；
//   - conhost 退出瞬间可能仍在向输出管道写最后一批数据，此刻关闭管道
//     会与写入并发（堆损坏 0xc0000374），故 Wait 后留 flush 宽限期。
const (
	drainGrace = 300 * time.Millisecond // Wait 后等待 conhost 完成末批输出
	forceAfter = 5 * time.Second        // 看门狗：僵死进程时强制释放止损
)

// ErrSessionClosed 会话已关闭后再次操作时返回。
var ErrSessionClosed = errors.New("terminal: 会话已关闭")

// Session 一次终端会话：一个 ConPTY 与一个 typeai.exe 子进程的绑定。
//
// 并发规则：Write/Resize/Close 可多线程调用；OnData 仅由内部读 goroutine
// 串行回调；OnExit 仅在子进程退出时回调一次（Close 触发的退出同样回调）。
type Session struct {
	pty pty.Pty
	cmd *pty.Cmd

	mu       sync.Mutex
	closed   bool // Close 已请求（幂等标志，立即生效不依赖收尾完成）
	finished bool // pump 读循环已退出（收尾完成）

	// waiter 写入一次、pump 读取；均在 mu 保护下。
	exitCode int
	exitErr  error

	OnData func(chunk []byte)        // PTY → 前端输出（读 goroutine 串行调用）
	OnExit func(code int, err error) // 子进程退出（仅一次；Close 主动终止时 code 非 0）
}

// Start 创建 ConPTY 并拉起 exePath（typeai.exe）。
// cols/rows 为初始终端尺寸，非正值时使用默认 80x25。
func Start(exePath string, cols, rows int) (*Session, error) {
	if exePath == "" {
		return nil, errors.New("terminal: 可执行文件路径为空")
	}
	if info, err := os.Stat(exePath); err != nil || info.IsDir() {
		return nil, fmt.Errorf("terminal: 可执行文件不可访问: %s", exePath)
	}
	if cols <= 0 {
		cols = defaultCols
	}
	if rows <= 0 {
		rows = defaultRows
	}

	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("terminal: 创建 ConPTY 失败: %w", err)
	}
	// 先设尺寸再启动，避免子进程以 80x25 起屏后再跳变
	if err := p.Resize(cols, rows); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("terminal: 设置初始尺寸失败: %w", err)
	}

	c := p.Command(exePath)
	s := &Session{pty: p, cmd: c}

	if err := c.Start(); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("terminal: 启动 %s 失败: %w", exePath, err)
	}

	go s.pump()
	go s.waiter()
	return s, nil
}

// waiter 收尸 goroutine：等子进程退出并记录退出信息，经 flush 宽限期后
// 关闭 PTY。关闭动作会唤醒 pump 的阻塞 Read——顺序保证尾包数据不丢、
// 且关闭时读循环已停在安全点之外由宽限期隔离写入竞争。
func (s *Session) waiter() {
	werr := s.cmd.Wait()
	var code int
	if s.cmd.ProcessState != nil {
		code = s.cmd.ProcessState.ExitCode()
	}
	s.mu.Lock()
	s.exitCode = code
	s.exitErr = werr
	s.mu.Unlock()

	time.Sleep(drainGrace)
	_ = s.pty.Close()
}

// pump 读 goroutine：PTY → OnData 转发；读到 EOF/错误后回调 OnExit（仅一次）。
func (s *Session) pump() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 && s.OnData != nil {
			chunk := make([]byte, n) // 复制后交出，buf 立即复用
			copy(chunk, buf[:n])
			s.OnData(chunk)
		}
		if err != nil {
			break
		}
	}

	s.mu.Lock()
	s.finished = true
	code, werr := s.exitCode, s.exitErr
	s.mu.Unlock()

	if s.OnExit != nil {
		s.OnExit(code, werr)
	}
}

// Write 将前端输入写入 PTY。
func (s *Session) Write(data []byte) error {
	if s.isClosed() {
		return ErrSessionClosed
	}
	if _, err := s.pty.Write(data); err != nil {
		return fmt.Errorf("terminal: 写入失败: %w", err)
	}
	return nil
}

// Resize 同步终端尺寸（列, 行）到 ConPTY；ConPTY 会触发子进程
// 的缓冲区变化事件，Bubble Tea 自行感知并重绘。
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return errors.New("terminal: 尺寸必须为正")
	}
	if s.isClosed() {
		return ErrSessionClosed
	}
	if err := s.pty.Resize(cols, rows); err != nil {
		return fmt.Errorf("terminal: 调整尺寸失败: %w", err)
	}
	return nil
}

// Close 终止会话：置位标志、杀子进程，PTY 的实际关闭由 waiter 在
// Wait + flush 宽限期后执行。幂等。typeai 单进程无守护（README 明确），
// 杀单进程即为完整清理；看门狗兜底僵死进程（forceAfter 后强关 PTY）。
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	go func() {
		time.Sleep(forceAfter)
		s.mu.Lock()
		done := s.finished
		s.mu.Unlock()
		if !done {
			_ = s.pty.Close() // 僵死止损：读循环大概率已被唤醒，竞争风险可接受
		}
	}()
}

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}
