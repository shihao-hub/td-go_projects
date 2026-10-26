// console_windows.go 按需显示/隐藏已运行进程的控制台窗口。
//
// 隐藏控制台由 start.go 的 SW_HIDE 创建；显示/隐藏不能在外部直接改目标进程，
// 这里用 AttachConsole(pid) 临时附着到目标控制台，再 ShowWindow 操作其窗口。
// clictl-cli 自身可能已经附着在用户终端上，因此实际动作交给一个短命的
// __console-ctl 子进程执行；GUI 进程没有控制台，可直接本地执行。
package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

var (
	procAttachConsole       = kernel32.NewProc("AttachConsole")
	procFreeConsole         = kernel32.NewProc("FreeConsole")
	procGetConsoleWindow    = kernel32.NewProc("GetConsoleWindow")
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
)

// SetConsoleVisible 显示或隐藏指定进程的控制台窗口，不重启进程。
// 刚启动的进程控制台可能尚未就绪，失败时短暂重试（最多 5 次 × 100ms）。
func SetConsoleVisible(pid int, visible bool) error {
	action := "hide"
	if visible {
		action = "show"
	}
	var last error
	for i := 0; i < 5; i++ {
		last = setConsoleVisibleOnce(pid, action)
		if last == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return last
}

func setConsoleVisibleOnce(pid int, action string) error {
	if helper := consoleCtlExe(); helper != "" {
		cmd := exec.Command(helper, "__console-ctl", strconv.Itoa(pid), action)
		cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg != "" {
				return fmt.Errorf("控制台操作失败: %v（%s）", err, msg)
			}
			return fmt.Errorf("控制台操作失败: %v", err)
		}
		return nil
	}
	_, err := consoleCtlLocal(pid, action)
	return err
}

// ConsoleVisible 查询指定进程的控制台窗口是否可见。
func ConsoleVisible(pid int) (bool, error) {
	if helper := consoleCtlExe(); helper != "" {
		out, err := exec.Command(helper, "__console-ctl", strconv.Itoa(pid), "status").
			Output()
		if err != nil {
			return false, err
		}
		switch strings.TrimSpace(string(out)) {
		case "visible":
			return true, nil
		case "hidden":
			return false, nil
		default:
			return false, fmt.Errorf("未知的控制台状态: %s", strings.TrimSpace(string(out)))
		}
	}
	return consoleCtlLocal(pid, "status")
}

// RunConsoleCtl 内部子命令：clictl-cli __console-ctl <pid> show|hide|status
func RunConsoleCtl(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "用法: clictl __console-ctl <pid> show|hide|status")
		return 2
	}
	pid, err := strconv.Atoi(args[0])
	if err != nil || pid <= 0 {
		fmt.Fprintln(os.Stderr, "非法 PID: "+args[0])
		return 2
	}
	state, err := consoleCtlLocal(pid, args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 2
	}
	if args[1] == "status" {
		if state {
			fmt.Println("visible")
		} else {
			fmt.Println("hidden")
		}
	}
	return 0
}

// consoleCtlLocal 在当前进程内附着目标控制台并执行动作。
// 调用前先 FreeConsole，避免 clictl-cli 自身终端被占用；
// 动作完成后立即 FreeConsole，目标控制台窗口不受影响。
func consoleCtlLocal(pid int, action string) (bool, error) {
	_, _, _ = procFreeConsole.Call()

	r1, _, err := procAttachConsole.Call(uintptr(pid))
	if r1 == 0 {
		return false, fmt.Errorf("附加控制台失败（PID %d）: %v", pid, err)
	}
	defer procFreeConsole.Call()

	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return false, fmt.Errorf("目标进程没有控制台（PID %d）", pid)
	}
	switch action {
	case "show":
		procShowWindow.Call(hwnd, uintptr(windows.SW_SHOW))
		procSetForegroundWindow.Call(hwnd)
		return true, nil
	case "hide":
		procShowWindow.Call(hwnd, uintptr(windows.SW_HIDE))
		return false, nil
	case "status":
		r, _, _ := procIsWindowVisible.Call(hwnd)
		return r != 0, nil
	default:
		return false, fmt.Errorf("未知控制台动作: %s", action)
	}
}

// consoleCtlExe 定位可执行 __console-ctl 的 CLI 产物：
//   - clictl-cli 自身 → 自己；
//   - clictl-gui → 同目录的 clictl-cli（gui 替换为 cli，保留 -dev 后缀）。
func consoleCtlExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	base := filepath.Base(exe)
	lower := strings.ToLower(base)
	idx := strings.Index(lower, "gui")
	if idx < 0 {
		return exe
	}
	candidate := filepath.Join(filepath.Dir(exe), base[:idx]+"cli"+base[idx+3:])
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
