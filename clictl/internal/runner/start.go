// start.go 后台分离启动：点火即走。
package runner

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"clictl/internal/store"

	"golang.org/x/sys/windows"
)

// 启动模式：detached = 无控制台后台分离（GUI/托盘/服务类）；
// console = 独立控制台窗口（命令行工具，用户可见、可交互）。
const (
	ModeDetached = "detached"
	ModeConsole  = "console"
)

// StartResult start 命令的 JSON 输出形状
type StartResult struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	PID            int    `json:"pid"`
	StartedAt      string `json:"started_at"`
	AlreadyRunning bool   `json:"already_running"` // 启动前已有活实例（仅提示，不拦截）
	Mode           string `json:"mode"`            // detached / console
}

// Start 后台分离启动：DETACHED_PROCESS（无控制台，GUI/托盘不闪黑框）+
// CREATE_NEW_PROCESS_GROUP（独立进程组，与 clictl 生命周期彻底解耦）。
// 不接任何 stdio、不 Wait、退出码无从谈起——记录只落 started_at + pid，
// 永不闭环（duration_ms/exit_code 恒 NULL），是否仍在运行由探活判定。
// 与 run 对比：要看输出、要等结果的用 run；点火就走的用 start。
func Start(st *store.Store, name string, args []string) (StartResult, *RunError) {
	tool, rerr := lookup(st, name)
	if rerr != nil {
		return StartResult{}, rerr
	}

	// 已有活实例提示（多实例自由，不拦截）
	unfinished, err := st.UnfinishedStarts(tool.ID)
	if err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "查询启动记录失败: " + err.Error(), ExitCode: 1}
	}
	alreadyRunning := len(FilterAlive(unfinished, tool.Path)) > 0

	start := time.Now()
	launchID, err := st.InsertLaunch(tool.ID, start)
	if err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "写入启动记录失败: " + err.Error(), ExitCode: 1}
	}

	cmd := exec.Command(tool.Path, args...)
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		// 启动失败也留痕：回写 127（无法执行）
		_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), 127)
		return StartResult{}, &RunError{Code: "start_failed", Message: "启动失败: " + err.Error(), ExitCode: 127}
	}
	pid := cmd.Process.Pid
	if err := st.SetLaunchPID(launchID, pid); err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "回写 PID 失败: " + err.Error(), ExitCode: 1}
	}

	return StartResult{
		Name:           tool.Name,
		Path:           tool.Path,
		PID:            pid,
		StartedAt:      start.UTC().Format(store.TimeFmt),
		AlreadyRunning: alreadyRunning,
		Mode:           ModeDetached,
	}, nil
}

// StartConsole 为命令行工具开独立控制台窗口启动（CREATE_NEW_CONSOLE），
// 后台等待退出并闭环记录。
//
// 不能用 os/exec：它会把未设置的 stdin/stdout/stderr 替换为 os.DevNull 并
// 设置 STARTF_USESTDHANDLES，导致新窗口里的 stdout 不是 TTY（isTTY=false），
// 依赖终端的 TUI 会误判为非交互模式后立即退出。这里用原生 CreateProcess，
// StartupInfo.Flags 保持 0，由系统把标准句柄初始化到新控制台。
//
// 进程本身仍是分离的（不挂 clictl 生命周期）；窗口关闭即进程结束，
// WaitForSingleObject 返回后回写 duration/exit_code。pid 为真实 exe，
// 探活与 stop 树杀语义与后台启动一致。
func StartConsole(st *store.Store, name string, args []string) (StartResult, *RunError) {
	tool, rerr := lookup(st, name)
	if rerr != nil {
		return StartResult{}, rerr
	}

	unfinished, err := st.UnfinishedStarts(tool.ID)
	if err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "查询启动记录失败: " + err.Error(), ExitCode: 1}
	}
	alreadyRunning := len(FilterAlive(unfinished, tool.Path)) > 0

	start := time.Now()
	launchID, err := st.InsertLaunch(tool.ID, start)
	if err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "写入启动记录失败: " + err.Error(), ExitCode: 1}
	}

	pid, procHandle, err := spawnConsoleProcess(tool.Path, args)
	if err != nil {
		_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), 127)
		return StartResult{}, &RunError{Code: "start_failed", Message: "启动失败: " + err.Error(), ExitCode: 127}
	}
	if err := st.SetLaunchPID(launchID, pid); err != nil {
		_ = windows.CloseHandle(procHandle)
		return StartResult{}, &RunError{Code: "db_error", Message: "回写 PID 失败: " + err.Error(), ExitCode: 1}
	}

	go func() {
		defer windows.CloseHandle(procHandle)
		_, _ = windows.WaitForSingleObject(procHandle, windows.INFINITE)
		var exitCode uint32
		if err := windows.GetExitCodeProcess(procHandle, &exitCode); err != nil {
			exitCode = 1
		}
		_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), int(exitCode))
	}()

	return StartResult{
		Name:           tool.Name,
		Path:           tool.Path,
		PID:            pid,
		StartedAt:      start.UTC().Format(store.TimeFmt),
		AlreadyRunning: alreadyRunning,
		Mode:           ModeConsole,
	}, nil
}

// spawnConsoleProcess 以原生 CreateProcess 启动独立控制台进程。
//
// 关键点：StartupInfo.Flags = 0（不设 STARTF_USESTDHANDLES），
// InheritHandles = false。系统会为新控制台初始化标准输入/输出句柄，
// 子进程的 stdout.isTTY 才为 true。返回进程句柄由调用方负责等待与关闭；
// thread 句柄在此立即关闭。
func spawnConsoleProcess(exePath string, args []string) (int, windows.Handle, error) {
	appName, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return 0, 0, err
	}

	parts := make([]string, 0, len(args)+1)
	parts = append(parts, syscall.EscapeArg(exePath))
	for _, arg := range args {
		parts = append(parts, syscall.EscapeArg(arg))
	}
	cmdLine, err := windows.UTF16PtrFromString(strings.Join(parts, " "))
	if err != nil {
		return 0, 0, err
	}

	// 工作目录设为 exe 所在目录，贴近资源管理器双击的行为。
	var currentDir *uint16
	if dir := filepath.Dir(exePath); dir != "" {
		if p, err := windows.UTF16PtrFromString(dir); err == nil {
			currentDir = p
		}
	}

	si := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(
		appName,
		cmdLine,
		nil,
		nil,
		false,
		windows.CREATE_NEW_CONSOLE,
		nil,
		currentDir,
		&si,
		&pi,
	); err != nil {
		return 0, 0, err
	}

	_ = windows.CloseHandle(pi.Thread)
	return int(pi.ProcessId), pi.Process, nil
}

// lookup run/start/stop 共用的前置校验：查注册 + 现场校验文件有效性 + 未注册相似名建议
func lookup(st *store.Store, name string) (store.Tool, *RunError) {
	tool, err := st.GetTool(name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			msg := "未注册的工具: " + name
			var suggestions []string
			if tools, lerr := st.ListTools(""); lerr == nil {
				names := make([]string, 0, len(tools))
				for _, t := range tools {
					names = append(names, t.Name)
				}
				if s := suggest(name, names, 3); len(s) > 0 {
					suggestions = s
					msg += "，是否想找: " + strings.Join(s, " / ")
				}
			}
			return store.Tool{}, &RunError{Code: "not_found", Message: msg, ExitCode: 127, Suggestions: suggestions}
		}
		return store.Tool{}, &RunError{Code: "db_error", Message: err.Error(), ExitCode: 1}
	}
	if fi, statErr := os.Stat(tool.Path); statErr != nil || fi.IsDir() {
		_ = st.RefreshStatus(tool.ID, false)
		return store.Tool{}, &RunError{Code: "invalid", Message: "文件已失效: " + tool.Path, ExitCode: 127}
	}
	return tool, nil
}
