//go:build windows

package browse

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// 进程创建标志：DETACHED（脱离控制台）+ 新进程组，点火即走不随 CLI 退出
const (
	detachedProcess     = 0x00000008
	createNewProcessGrp = 0x00000200
	createNoWindow      = 0x08000000
	stillActiveExitCode = 259
)

// lookZread 定位 zread 命令（npm 全局安装）
func lookZread() (string, error) {
	return exec.LookPath("zread")
}

// spawnDetached 后台分离启动：cmd.exe /c zread browse ...，
// stdout/stderr 追加到临时目录日志，返回 pid。
func spawnDetached(zreadCmd string, opt Options) (int, error) {
	logPath := filepath.Join(os.TempDir(), "zreadmanager.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("打开日志文件失败: %w", err)
	}
	defer logFile.Close()

	args := []string{"browse"}
	if opt.Host != "" {
		args = append(args, "--host", opt.Host)
	}
	if opt.Port > 0 {
		args = append(args, "--port", strconv.Itoa(opt.Port))
	}
	if opt.Generate {
		args = append(args, "--generate")
	}

	cmd := exec.Command("cmd.exe", append([]string{"/c", zreadCmd}, args...)...)
	cmd.Dir = opt.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | createNewProcessGrp,
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile

	fmt.Fprintf(logFile, "\n===== %s zreadmanager 启动 zread browse (dir=%s) =====\n",
		time.Now().Format("2006-01-02 15:04:05"), opt.Dir)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// alive PID 探活三重校验：
// OpenProcess 存在性 + GetExitCodeProcess 终止态 + 创建时间比对（防 PID 回收复用）。
// createUnix <= 0 时跳过创建时间比对（宽容）。
func alive(pid int, createUnix int64) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	if exitCode != stillActiveExitCode {
		return false
	}
	if createUnix <= 0 {
		return true
	}
	cur, err := processCreateUnix(pid)
	if err != nil || cur != createUnix {
		return false
	}
	return true
}

// processCreateUnix 读进程创建时间（unix 秒），PID 复用时会发生变化
func processCreateUnix(pid int) (int64, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)

	var creation, exitTime, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exitTime, &kernel, &user); err != nil {
		return 0, err
	}
	// FILETIME：1601-01-01 起 100ns 份数（高 32 位 << 32 | 低 32 位）→ unix 秒
	ft := uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
	return int64(ft/1_000_0000) - 11644473600, nil
}

// killTree taskkill 树杀（cmd.exe /c zread 会拉起 node 子进程链）
func killTree(pid int) error {
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	_, err := cmd.Output()
	return err
}
