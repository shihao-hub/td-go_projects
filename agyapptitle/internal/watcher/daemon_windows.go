package watcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"

	"language_projects/go_projects/agyapptitle/internal/config"
)

func getPIDFilePath() string {
	return filepath.Join(config.GetAppDataDir(), "daemon.pid")
}

// StartDaemon 在 Windows 下以无窗口（零黑框）后台模式拉起本程序的 watch 子进程。
func StartDaemon() (int, error) {
	if status, pid := CheckDaemonRunning(); status {
		return pid, fmt.Errorf("daemon is already running (PID: %d)", pid)
	}

	exePath, err := os.Executable()
	if err != nil {
		return 0, err
	}

	cmd := exec.Command(exePath, "watch")
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS,
	}

	logFile, err := os.OpenFile(filepath.Join(config.GetAppDataDir(), "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		return 0, err
	}

	pid := cmd.Process.Pid
	_ = os.WriteFile(getPIDFilePath(), []byte(strconv.Itoa(pid)), 0644)
	return pid, nil
}

// StopDaemon 优雅终止正在运行的后台守护进程。
func StopDaemon() error {
	running, pid := CheckDaemonRunning()
	if !running {
		_ = os.Remove(getPIDFilePath())
		return fmt.Errorf("daemon is not running")
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(getPIDFilePath())
		return err
	}

	_ = proc.Kill()
	_ = os.Remove(getPIDFilePath())
	return nil
}

// CheckDaemonRunning 检查后台守护进程是否活跃。
func CheckDaemonRunning() (bool, int) {
	b, err := os.ReadFile(getPIDFilePath())
	if err != nil {
		return false, 0
	}

	pidStr := strings.TrimSpace(string(b))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false, 0
	}

	hProcess, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false, 0
	}
	defer windows.CloseHandle(hProcess)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(hProcess, &exitCode); err == nil {
		if exitCode == 259 { // STILL_ACTIVE
			return true, pid
		}
	}

	return false, 0
}
