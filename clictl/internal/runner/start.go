// start.go 后台分离/隐藏控制台/可见控制台启动。
package runner

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"clictl/internal/store"

	"golang.org/x/sys/windows"
)

// 启动模式：console = 可见控制台；hidden = 隐藏控制台（可显示/隐藏）；
// detached = 完全无控制台。
const (
	ModeDetached = store.ModeDetached
	ModeConsole  = store.ModeConsole
	ModeHidden   = store.ModeHidden
)

// StartResult start 命令的 JSON 输出形状
type StartResult struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	Target         string `json:"target"`
	PID            int    `json:"pid"`
	StartedAt      string `json:"started_at"`
	AlreadyRunning bool   `json:"already_running"` // 启动前已有同目标活实例（仅提示，不拦截）
	Mode           string `json:"mode"`            // console / hidden / detached
}

// Start 后台分离启动（DETACHED，无控制台）
func Start(st *store.Store, name, target string, args []string) (StartResult, *RunError) {
	return StartWithMode(st, name, target, ModeDetached, args)
}

// StartConsole 在独立控制台窗口启动（可见）
func StartConsole(st *store.Store, name, target string, args []string) (StartResult, *RunError) {
	return StartWithMode(st, name, target, ModeConsole, args)
}

// StartHiddenConsole 在隐藏控制台启动（可随时显示/隐藏）
func StartHiddenConsole(st *store.Store, name, target string, args []string) (StartResult, *RunError) {
	return StartWithMode(st, name, target, ModeHidden, args)
}

// StartWithMode 按显式模式启动；mode 为空时按目标类型自动选择：
// exe console → console，exe GUI → detached；源码 console → console，源码 gui → hidden。
func StartWithMode(st *store.Store, name, target, mode string, args []string) (StartResult, *RunError) {
	tool, rerr := lookupTool(st, name)
	if rerr != nil {
		return StartResult{}, rerr
	}
	spec, rerr := BuildSpec(tool, target, args)
	if rerr != nil {
		return StartResult{}, rerr
	}

	actual := mode
	if actual == "" {
		if spec.Target == store.TargetSource {
			if spec.Console {
				actual = ModeConsole
			} else {
				actual = ModeHidden
			}
		} else if spec.Console {
			actual = ModeConsole
		} else {
			actual = ModeDetached
		}
	}

	unfinished, err := st.UnfinishedStarts(tool.ID)
	if err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "查询启动记录失败: " + err.Error(), ExitCode: 1}
	}
	alreadyRunning := len(aliveForTarget(unfinished, tool, spec.Target)) > 0

	start := time.Now()
	launchID, err := st.InsertLaunch(tool.ID, start, spec.Target, actual)
	if err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "写入启动记录失败: " + err.Error(), ExitCode: 1}
	}

	var pid int
	switch actual {
	case ModeDetached:
		pid, rerr = startDetached(spec)
	case ModeConsole:
		if spec.Target == store.TargetExe && !spec.Console {
			_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), 127)
			return StartResult{}, &RunError{Code: "console_unavailable", Message: "该 exe 是窗口程序，无法在控制台启动", ExitCode: 1}
		}
		pid, rerr = startConsole(st, launchID, start, spec, false)
	case ModeHidden:
		if spec.Target == store.TargetExe && !spec.Console {
			_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), 127)
			return StartResult{}, &RunError{Code: "console_unavailable", Message: "该 exe 是窗口程序，没有可隐藏的控制台", ExitCode: 1}
		}
		pid, rerr = startConsole(st, launchID, start, spec, true)
	default:
		_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), 127)
		return StartResult{}, &RunError{Code: "bad_args", Message: "mode 仅支持 console | hidden | detached", ExitCode: 1}
	}
	if rerr != nil {
		return StartResult{}, rerr
	}

	if err := st.SetLaunchPID(launchID, pid); err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "回写 PID 失败: " + err.Error(), ExitCode: 1}
	}

	return StartResult{
		Name:           tool.Name,
		Path:           spec.Program,
		Target:         spec.Target,
		PID:            pid,
		StartedAt:      start.UTC().Format(store.TimeFmt),
		AlreadyRunning: alreadyRunning,
		Mode:           actual,
	}, nil
}

// startDetached 后台分离：DETACHED_PROCESS（无控制台）
func startDetached(spec LaunchSpec) (int, *RunError) {
	cmd := exec.Command(spec.Program, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return 0, &RunError{Code: "start_failed", Message: "启动失败: " + err.Error(), ExitCode: 127}
	}
	return cmd.Process.Pid, nil
}

// startConsole 用原生 CreateProcess 启动独立控制台；hidden 时窗口隐藏。
// 进程本身仍是分离的（不挂 clictl 生命周期）；窗口关闭即进程结束，
// WaitForSingleObject 返回后回写 duration/exit_code。
func startConsole(st *store.Store, launchID int64, start time.Time, spec LaunchSpec, hidden bool) (int, *RunError) {
	pid, procHandle, err := spawnConsoleProcess(spec.Program, spec.Args, spec.Dir, hidden)
	if err != nil {
		_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), 127)
		return 0, &RunError{Code: "start_failed", Message: "启动失败: " + err.Error(), ExitCode: 127}
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
	return pid, nil
}

// spawnConsoleProcess 以原生 CreateProcess 启动独立控制台进程。
//
// 关键点：可见时 StartupInfo.Flags = 0（不设 STARTF_USESTDHANDLES），
// InheritHandles = false。系统会为新控制台初始化标准输入/输出句柄，
// 子进程的 stdout.isTTY 才为 true。hidden 时用 SW_HIDE 隐藏控制台窗口。
func spawnConsoleProcess(program string, args []string, dir string, hidden bool) (int, windows.Handle, error) {
	appName, err := windows.UTF16PtrFromString(program)
	if err != nil {
		return 0, 0, err
	}

	parts := make([]string, 0, len(args)+1)
	parts = append(parts, syscall.EscapeArg(program))
	for _, arg := range args {
		parts = append(parts, syscall.EscapeArg(arg))
	}
	cmdLine, err := windows.UTF16PtrFromString(strings.Join(parts, " "))
	if err != nil {
		return 0, 0, err
	}

	currentDir := dir
	if currentDir == "" {
		currentDir = filepath.Dir(program)
	}
	var currentDirPtr *uint16
	if currentDir != "" {
		if p, err := windows.UTF16PtrFromString(currentDir); err == nil {
			currentDirPtr = p
		}
	}

	si := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	flags := uint32(windows.CREATE_NEW_CONSOLE)
	if hidden {
		flags |= windows.CREATE_NEW_PROCESS_GROUP
		si.Flags = windows.STARTF_USESHOWWINDOW
		si.ShowWindow = uint16(windows.SW_HIDE)
	}
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(
		appName,
		cmdLine,
		nil,
		nil,
		false,
		flags,
		nil,
		currentDirPtr,
		&si,
		&pi,
	); err != nil {
		return 0, 0, err
	}

	_ = windows.CloseHandle(pi.Thread)
	return int(pi.ProcessId), pi.Process, nil
}

// aliveForTarget 过滤出指定目标的存活实例（旧记录 target 为空时按默认目标处理）
func aliveForTarget(launches []store.Launch, tool store.Tool, target string) []store.Launch {
	alive := FilterAliveForTool(launches, tool)
	out := make([]store.Launch, 0, len(alive))
	for _, l := range alive {
		lt := l.Target
		if lt == "" {
			lt = tool.DefaultTarget
		}
		if lt == target {
			out = append(out, l)
		}
	}
	return out
}
