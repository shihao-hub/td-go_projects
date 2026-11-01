package watcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"

	"language_projects/go_projects/agyapptitle/internal/buildinfo"
	"language_projects/go_projects/agyapptitle/internal/config"
)

func getPIDFilePath() string {
	return filepath.Join(config.GetAppDataDir(), "daemon.pid")
}

// EnsureAppDataDir 保证数据目录存在。
func EnsureAppDataDir() error {
	return os.MkdirAll(config.GetAppDataDir(), 0755)
}

// StartDaemon 在 Windows 下通过 WMI 进程脱离模式拉起本程序的无黑框后台常驻 watch 进程。
func StartDaemon() (int, error) {
	if err := EnsureAppDataDir(); err != nil {
		return 0, fmt.Errorf("create appdata dir failed: %w", err)
	}

	if status, pid := CheckDaemonRunning(); status {
		return pid, fmt.Errorf("daemon is already running (PID: %d)", pid)
	}

	exePath, err := os.Executable()
	if err != nil {
		return 0, err
	}

	dir := filepath.Dir(exePath)
	targetExe := exePath

	// 优先寻找对应的无黑框镜像 (dev 下找 agyapptitlew-dev.exe，release 下找 agyapptitlew.exe)
	var candidateName string
	if buildinfo.IsRelease() {
		candidateName = "agyapptitlew.exe"
	} else {
		candidateName = "agyapptitlew-dev.exe"
	}
	candidatePath := filepath.Join(dir, candidateName)
	if _, err := os.Stat(candidatePath); err == nil {
		targetExe = candidatePath
	}

	cmdLine := fmt.Sprintf(`"%s" watch`, targetExe)
	escapedCmdLine := strings.ReplaceAll(cmdLine, `'`, `''`)
	escapedDir := strings.ReplaceAll(dir, `'`, `''`)

	// 1. 优先通过 WMI 启动，彻底脱离父控制台 Job Object 作业树连坐限制
	psScript := fmt.Sprintf(
		`$res = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine='%s'; CurrentDirectory='%s'}; if ($res.ReturnValue -eq 0) { $res.ProcessId } else { exit 1 }`,
		escapedCmdLine,
		escapedDir,
	)

	ps := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", psScript)
	ps.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := ps.Output()
	if err == nil {
		pidStr := strings.TrimSpace(string(out))
		if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
			_ = os.WriteFile(getPIDFilePath(), []byte(strconv.Itoa(pid)), 0644)
			return pid, nil
		}
	}

	// 2. 兜底机制：带 DETACHED_PROCESS 与 CREATE_NEW_PROCESS_GROUP 标志直接拉起
	cmd := exec.Command(targetExe, "watch")
	cmd.Dir = dir
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}

	logPath := filepath.Join(config.GetAppDataDir(), "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		return 0, err
	}

	pid := cmd.Process.Pid
	if err := os.WriteFile(getPIDFilePath(), []byte(strconv.Itoa(pid)), 0644); err != nil {
		_ = cmd.Process.Kill()
		return 0, fmt.Errorf("write pid file failed: %w", err)
	}
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
