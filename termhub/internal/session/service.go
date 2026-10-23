// Package session 是 termhub 的核心接缝：SessionManager。
// 进程管理、终端流的事件桥推送、历史读写与 agent 诊断都从这里发起。
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/shihao-hub/termhub/internal/pty"
	"github.com/shihao-hub/termhub/internal/store"
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
	// Source 会话来源：manual / entry:<id> / group:<id>。
	Source string
	// WorkDir 会话初始工作目录。
	WorkDir string

	mu      sync.Mutex
	pt      pty.Term
	cols    int
	rows    int
	buffer  []byte
	subs    map[chan []byte]struct{}
	stopped chan struct{}
}

// Service 终端会话管理器（测试接缝）。
type Service struct {
	Factory     pty.Factory
	CommandLine string // 默认 shell 命令行（无 Store 时使用）
	Store       *store.Store
	AgentCLI    string
	// AgentRunner 可注入的 agent 执行器；为空时调用本机 CLI。
	AgentRunner func(ctx context.Context, cli string, args []string) ([]byte, error)

	mu            sync.Mutex
	sessions      map[string]*Session
	seq           int
	conversations map[string]string
}

// NewService 构造会话管理器；factory 由调用方注入（真 ConPTY 或 fake）。
func NewService(factory pty.Factory) *Service {
	return &Service{
		Factory:       factory,
		CommandLine:   "powershell.exe -NoLogo",
		AgentCLI:      "agy",
		sessions:      make(map[string]*Session),
		conversations: make(map[string]string),
	}
}

// NewServiceWithStore 携带持久化存储的会话管理器。
func NewServiceWithStore(factory pty.Factory, st *store.Store) *Service {
	svc := NewService(factory)
	svc.Store = st
	return svc
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

	s.mu.Lock()
	s.seq++
	id := fmt.Sprintf("s%03d-%d", s.seq, time.Now().UnixMilli())
	s.mu.Unlock()

	commandLine, err := s.prepareCommandLine(id, opts)
	if err != nil {
		return "", err
	}
	opts.CommandLine = commandLine

	t, err := s.Factory.Start(opts)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
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

// prepareCommandLine 为真实会话生成带历史钩子的 PowerShell 启动命令。
func (s *Service) prepareCommandLine(id string, opts pty.Options) (string, error) {
	if opts.CommandLine != "" {
		return opts.CommandLine, nil
	}
	if s.Store == nil {
		return s.CommandLine, nil
	}
	history := s.Store.History()
	if err := history.Prepare(id); err != nil {
		return "", fmt.Errorf("准备会话历史失败: %w", err)
	}
	bootstrap := history.BootstrapPath(id)
	script := buildBootstrap(opts.WorkDir, history.SessionPath(id), history.SessionCwdPath(id), opts.InitialCommand)
	if err := os.WriteFile(bootstrap, []byte(script), 0o644); err != nil {
		return "", fmt.Errorf("写会话启动脚本失败: %w", err)
	}
	return fmt.Sprintf(`powershell.exe -NoLogo -NoExit -File "%s"`, bootstrap), nil
}

// buildBootstrap 生成注入 prompt 钩子与初始命令的启动脚本。
func buildBootstrap(workDir, historyPath, cwdPath, initialCommand string) string {
	var b strings.Builder
	b.WriteString("# termhub 会话启动脚本（自动生成）\n")
	if workDir != "" {
		fmt.Fprintf(&b, "Set-Location -LiteralPath '%s' -ErrorAction SilentlyContinue\n", psQuote(workDir))
	}
	fmt.Fprintf(&b, "$global:__termhub_history = '%s'\n", psQuote(historyPath))
	fmt.Fprintf(&b, "$global:__termhub_cwd = '%s'\n", psQuote(cwdPath))
	b.WriteString(`$global:__termhub_enc = New-Object System.Text.UTF8Encoding($false)
function global:__termhub_log([string]$text) {
  if (-not $text) { return }
  $flat = $text -replace "` + "`" + `r?` + "`" + `n", " "
  [System.IO.File]::AppendAllText($global:__termhub_history, $flat + "` + "`" + `r` + "`" + `n", $global:__termhub_enc)
}
Import-Module PSReadLine -ErrorAction SilentlyContinue
$global:__termhub_readline = $function:PSConsoleHostReadLine
if ($global:__termhub_readline) {
  function global:PSConsoleHostReadLine {
    $line = & $global:__termhub_readline
    if ($line) { __termhub_log $line }
    return $line
  }
} else {
  $global:__termhub_prompt = $function:prompt
  function global:prompt {
    $last = Get-History -Count 1 -ErrorAction SilentlyContinue
    if ($last -and $last.Id -ne $global:__termhub_lastid) {
      $global:__termhub_lastid = $last.Id
      __termhub_log $last.CommandLine
    }
    if ($global:__termhub_prompt) { & $global:__termhub_prompt } else { "PS $($executionContext.SessionState.Path.CurrentLocation)> " }
  }
}
`)
	b.WriteString(`$global:__termhub_orig_prompt = $function:prompt
[System.IO.File]::WriteAllText($global:__termhub_cwd, (Get-Location).Path, $global:__termhub_enc)
function global:prompt {
  try { [System.IO.File]::WriteAllText($global:__termhub_cwd, $executionContext.SessionState.Path.CurrentLocation.Path, $global:__termhub_enc) } catch { }
  if ($global:__termhub_orig_prompt) { & $global:__termhub_orig_prompt } else { "PS $($executionContext.SessionState.Path.CurrentLocation)> " }
}
`)
	if initialCommand != "" {
		fmt.Fprintf(&b, "$global:__termhub_initial = '%s'\n", psQuote(initialCommand))
		b.WriteString("__termhub_log $global:__termhub_initial\n")
		b.WriteString("Invoke-Expression $global:__termhub_initial\n")
	}
	return b.String()
}

func psQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
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

// finish 会话收尾：更新状态、合并历史、持久化元数据并广播退出事件。
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
	lastOutput := stripANSI(string(sess.buffer))
	sess.mu.Unlock()

	commands := []string(nil)
	if s.Store != nil {
		history := s.Store.History()
		commands = history.ReadSession(sess.ID)
		if len(commands) > 0 {
			_ = history.AppendGlobal(commands)
		}
		_ = s.Store.AppendSession(store.SessionRecord{
			ID:         sess.ID,
			Source:     sess.Source,
			WorkDir:    sess.WorkDir,
			Cwd:        s.sessionCwd(sess),
			StartedAt:  sess.StartedAt,
			ExitedAt:   sess.ExitedAt,
			ExitCode:   sess.ExitCode,
			Status:     string(StatusExited),
			Commands:   commands,
			LastOutput: truncate(lastOutput, 16*1024),
		})
	}
	close(sess.stopped)

	s.mu.Lock()
	delete(s.sessions, sess.ID)
	s.mu.Unlock()
}

// StopSession 终止指定会话的整棵进程树。
func (s *Service) StopSession(id string) error {
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
	return pt.Close()
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
	ID          string      `json:"id"`
	Status      Status      `json:"status"`
	StartedAt   time.Time   `json:"started_at"`
	ExitCode    int         `json:"exit_code"`
	WorkDir     string      `json:"work_dir"`
	Cwd         string      `json:"cwd"`
	Source      string      `json:"source"`
	MemoryBytes uint64      `json:"memory_bytes"`
	Commands    []string    `json:"commands"`
	Opts        pty.Options `json:"-"`
}

// ListSessions 返回全部运行中会话快照。
func (s *Service) ListSessions() []Snapshot {
	s.mu.Lock()
	sessions := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()

	out := make([]Snapshot, 0, len(sessions))
	for _, sess := range sessions {
		sess.mu.Lock()
		pt := sess.pt
		sess.mu.Unlock()
		var memory uint64
		if pt != nil {
			memory = pt.MemoryBytes()
		}
		commands := []string(nil)
		if s.Store != nil {
			commands = s.Store.History().ReadSession(sess.ID)
		}
		out = append(out, Snapshot{
			ID:          sess.ID,
			Status:      sess.Status,
			StartedAt:   sess.StartedAt,
			ExitCode:    sess.ExitCode,
			WorkDir:     sess.WorkDir,
			Cwd:         s.sessionCwd(sess),
			Source:      sess.Source,
			MemoryBytes: memory,
			Commands:    commands,
		})
	}
	return out
}

// History 返回已退出会话记录。
func (s *Service) History() []store.SessionRecord {
	if s.Store == nil {
		return nil
	}
	return s.Store.Sessions()
}

// AskAgent 把最近命令与最后一屏输出交给本机 agy CLI，并返回追问会话 ID。
func (s *Service) AskAgent(ctx context.Context, id, question, conversationID string) (string, string, error) {
	commands, lastOutput, workDir, err := s.agentContext(id)
	if err != nil {
		return "", "", err
	}
	prompt := buildAgentPrompt(workDir, commands, lastOutput, question)

	cli := s.AgentCLI
	if cli == "" {
		cli = "agy"
	}
	args := []string{"-p", prompt, "--output-format", "json", "--mode", "plan", "--sandbox"}
	if conversationID != "" {
		args = append(args, "--conversation", conversationID)
	} else if existing := s.conversation(id); existing != "" {
		args = append(args, "--conversation", existing)
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var output []byte
	if s.AgentRunner != nil {
		output, err = s.AgentRunner(ctx, cli, args)
	} else {
		output, err = exec.CommandContext(ctx, cli, args...).Output()
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", "", fmt.Errorf("agent 调用超时")
		}
		return "", "", fmt.Errorf("agent 调用失败: %w", err)
	}

	var resp struct {
		ConversationID string `json:"conversation_id"`
		Response       string `json:"response"`
		Status         string `json:"status"`
	}
	text := strings.TrimSpace(string(output))
	if err := json.Unmarshal(output, &resp); err == nil && resp.Response != "" {
		text = resp.Response
		if resp.ConversationID != "" {
			s.setConversation(id, resp.ConversationID)
		}
	}
	return text, resp.ConversationID, nil
}

func (s *Service) agentContext(id string) ([]string, string, string, error) {
	s.mu.Lock()
	sess, live := s.sessions[id]
	s.mu.Unlock()
	if live {
		sess.mu.Lock()
		output := stripANSI(string(sess.buffer))
		sess.mu.Unlock()
		workDir := s.sessionCwd(sess)
		commands := []string(nil)
		if s.Store != nil {
			commands = s.Store.History().ReadSession(id)
		}
		return commands, output, workDir, nil
	}
	if s.Store != nil {
		if rec, ok := s.Store.Session(id); ok {
			workDir := rec.Cwd
			if workDir == "" {
				workDir = rec.WorkDir
			}
			return rec.Commands, rec.LastOutput, workDir, nil
		}
	}
	return nil, "", "", fmt.Errorf("会话不存在: %s", id)
}

func (s *Service) conversation(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conversations[id]
}

func (s *Service) setConversation(id, convo string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conversations[id] = convo
}

func buildAgentPrompt(workDir string, commands []string, lastOutput, question string) string {
	var b strings.Builder
	b.WriteString("你在分析一个 Windows PowerShell 终端会话。请基于以下上下文回答，必要时给出可执行的修复命令。\n\n")
	if workDir != "" {
		fmt.Fprintf(&b, "初始目录: %s\n", workDir)
	}
	b.WriteString("\n最近命令:\n")
	if len(commands) == 0 {
		b.WriteString("(无)\n")
	} else {
		start := 0
		if len(commands) > 20 {
			start = len(commands) - 20
		}
		for _, cmd := range commands[start:] {
			fmt.Fprintf(&b, "$ %s\n", cmd)
		}
	}
	b.WriteString("\n最后一屏输出:\n")
	if lastOutput == "" {
		b.WriteString("(无)\n")
	} else {
		b.WriteString(truncate(lastOutput, 8000))
		b.WriteString("\n")
	}
	if strings.TrimSpace(question) == "" {
		question = "请诊断最后一条命令失败的原因，并给出修复建议。"
	}
	b.WriteString("\n用户问题:\n")
	b.WriteString(question)
	return b.String()
}

var ansiPattern = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)

func stripANSI(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}

func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[len(text)-max:]
}

// sessionCwd 读取 shell 当前目录（prompt 钩子落盘），失败时回落初始目录。
func (s *Service) sessionCwd(sess *Session) string {
	if s.Store == nil {
		return sess.WorkDir
	}
	data, err := os.ReadFile(s.Store.History().SessionCwdPath(sess.ID))
	if err == nil {
		if cwd := strings.TrimSpace(string(data)); cwd != "" {
			return cwd
		}
	}
	return sess.WorkDir
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
	for _, sess := range sessions {
		select {
		case <-sess.stopped:
		case <-time.After(2 * time.Second):
		}
	}
}

// maxBuffer 每会话保留的最多输出字节（约十几屏）。
const maxBuffer = 256 * 1024
