package terminal

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"agysess/internal/model"
)

var convRegex = regexp.MustCompile(`(?i)--conversation(?:=|\s+)([a-zA-Z0-9_\-]+)`)

// RunningSessionInfo 运行中会话信息
type RunningSessionInfo struct {
	PID         int    `json:"pid"`
	SessionID   string `json:"session_id,omitempty"`
	CommandLine string `json:"command_line"`
}

// TerminalManager 负责 Windows Terminal 与 agy 进程的调度与生命周期管理
type TerminalManager struct {
	mu          sync.Mutex
	wtPath      string
	agyPath     string
	cacheTime   time.Time
	cachedPIDs  map[string]int // sessionID -> PID
	unnamedPIDs []int          // 没有 --conversation 参数的 PID
}

// NewTerminalManager 初始化管理器并检测外部程序依赖
func NewTerminalManager() (*TerminalManager, error) {
	mgr := &TerminalManager{
		cachedPIDs: make(map[string]int),
	}

	mgr.wtPath = findWTPath()
	mgr.agyPath = findAgyPath()

	return mgr, nil
}

// WTPath 返回探测到的 Windows Terminal 路径
func (m *TerminalManager) WTPath() string {
	return m.wtPath
}

// AgyPath 返回探测到的 agy CLI 路径
func (m *TerminalManager) AgyPath() string {
	return m.agyPath
}

// OpenConversation 在 Windows Terminal 中打开/恢复已有会话
func (m *TerminalManager) OpenConversation(ctx context.Context, session *model.Session, newWindow bool) error {
	if m.wtPath == "" {
		return fmt.Errorf("未在系统中检测到 Windows Terminal (wt.exe)，请确认已安装或在 PATH 中")
	}
	if m.agyPath == "" {
		return fmt.Errorf("未在系统中检测到 agy CLI (agy.exe)，请确认已安装")
	}

	workspace := session.WorkspaceURIs
	if workspace == "" || !dirExists(workspace) {
		// 回退使用当前工作目录
		if cwd, err := os.Getwd(); err == nil {
			workspace = cwd
		}
	}

	title := session.DisplayTitle()
	tabTitle := fmt.Sprintf("agy: %s", title)

	var args []string
	if newWindow {
		args = append(args, "new-window")
	} else {
		// -w 0 复用当前最顶层 WT 窗口新标签页，若无窗口则自动新建
		args = append(args, "-w", "0", "new-tab")
	}

	if workspace != "" {
		args = append(args, "-d", workspace)
	}
	args = append(args, "--title", tabTitle)
	args = append(args, m.agyPath, "--conversation", session.ID)

	cmd := exec.CommandContext(ctx, m.wtPath, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起 Windows Terminal 失败: %w", err)
	}

	return nil
}

// CreateNewConversation 在 Windows Terminal 中新建会话
func (m *TerminalManager) CreateNewConversation(ctx context.Context, opts model.CreateSessionOptions) error {
	if m.wtPath == "" {
		return fmt.Errorf("未在系统中检测到 Windows Terminal (wt.exe)")
	}
	if m.agyPath == "" {
		return fmt.Errorf("未在系统中检测到 agy CLI (agy.exe)")
	}

	workspace := opts.WorkspaceDir
	if workspace == "" || !dirExists(workspace) {
		if cwd, err := os.Getwd(); err == nil {
			workspace = cwd
		}
	}

	var args []string
	if opts.NewWindow {
		args = append(args, "new-window")
	} else {
		args = append(args, "-w", "0", "new-tab")
	}

	if workspace != "" {
		args = append(args, "-d", workspace)
	}
	args = append(args, "--title", "agy: 新会话")
	args = append(args, m.agyPath)

	if opts.ProjectID != "" {
		args = append(args, "--project", opts.ProjectID)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Mode != "" {
		args = append(args, "--mode", opts.Mode)
	}
	if opts.Effort != "" {
		args = append(args, "--effort", opts.Effort)
	}
	if opts.InitialPrompt != "" {
		args = append(args, "-i", opts.InitialPrompt)
	}

	cmd := exec.CommandContext(ctx, m.wtPath, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("新建 agy 终端会话失败: %w", err)
	}

	return nil
}

// DetectRunningSessions 探测当前活跃运行中的 agy 进程
func (m *TerminalManager) DetectRunningSessions(ctx context.Context) ([]RunningSessionInfo, error) {
	// 使用 powershell Get-CimInstance 快速探测进程命令行
	psScript := `Get-CimInstance Win32_Process -Filter "Name = 'agy.exe'" | ForEach-Object { "$($_.ProcessId)::$($_.CommandLine)" }`
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("探测系统进程失败: %w", err)
	}

	var results []RunningSessionInfo
	newMap := make(map[string]int)
	var unnamed []int

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "::", 2)
		if len(parts) < 2 {
			continue
		}

		pid, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}

		cmdLine := strings.TrimSpace(parts[1])
		var matchedSessionID string

		matches := convRegex.FindStringSubmatch(cmdLine)
		if len(matches) > 1 {
			matchedSessionID = matches[1]
			newMap[matchedSessionID] = pid
		} else {
			unnamed = append(unnamed, pid)
		}

		results = append(results, RunningSessionInfo{
			PID:         pid,
			SessionID:   matchedSessionID,
			CommandLine: cmdLine,
		})
	}

	m.mu.Lock()
	m.cachedPIDs = newMap
	m.unnamedPIDs = unnamed
	m.cacheTime = time.Now()
	m.mu.Unlock()

	return results, nil
}

// GetPIDForSession 获取某个会话当前活跃的 PID（命中缓存或即时探测）
func (m *TerminalManager) GetPIDForSession(sessionID string) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 若缓存新鲜（3秒内），直接查缓存
	if time.Since(m.cacheTime) < 3*time.Second {
		pid, ok := m.cachedPIDs[sessionID]
		return pid, ok
	}

	return 0, false
}

// KillSessionProcess 安全且彻底地关闭指定会话的终端进程树
func (m *TerminalManager) KillSessionProcess(ctx context.Context, sessionID string) (int, error) {
	// 刷新探测一次
	running, err := m.DetectRunningSessions(ctx)
	if err != nil {
		return 0, err
	}

	var targetPID int
	for _, r := range running {
		if r.SessionID == sessionID {
			targetPID = r.PID
			break
		}
	}

	if targetPID == 0 {
		return 0, fmt.Errorf("未检测到会话 [%s] 正在运行", sessionID)
	}

	// Windows 树级强制终止 taskkill /PID <targetPID> /T /F
	killCmd := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(targetPID), "/T", "/F")
	out, err := killCmd.CombinedOutput()
	if err != nil {
		return targetPID, fmt.Errorf("终止进程 PID %d 失败: %s (%w)", targetPID, string(out), err)
	}

	// 移除缓存
	m.mu.Lock()
	delete(m.cachedPIDs, sessionID)
	m.mu.Unlock()

	return targetPID, nil
}

// findWTPath 探测 Windows Terminal 绝对路径
func findWTPath() string {
	// 优先检查常见安装位置
	candidates := []string{
		`D:\Users\Softwares\WindowsTerminal\wt.exe`,
	}
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		candidates = append(candidates,
			filepath.Join(localAppData, "Microsoft", "WindowsApps", "wt.exe"),
		)
	}

	for _, p := range candidates {
		if fileExists(p) {
			return p
		}
	}

	// 查 PATH
	if p, err := exec.LookPath("wt.exe"); err == nil {
		return p
	}

	return ""
}

// findAgyPath 探测 agy.exe 绝对路径
func findAgyPath() string {
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		candidate := filepath.Join(localAppData, "agy", "bin", "agy.exe")
		if fileExists(candidate) {
			return candidate
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".gemini", "antigravity-cli", "bin", "agy.exe")
		if fileExists(candidate) {
			return candidate
		}
	}

	if p, err := exec.LookPath("agy.exe"); err == nil {
		return p
	}

	return ""
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
