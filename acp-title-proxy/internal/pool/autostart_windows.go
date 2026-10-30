//go:build windows

package pool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// LaunchDaemonInBackground 静默拉起 acp-title-proxy 守护进程，不弹出控制台窗口。
// 优先通过 WMI 创建进程（父进程为 WmiPrvSE.exe）并设置 SW_HIDE（ShowWindow = 0），
// 使守护进程完全脱离 Zed 的进程树，杜绝黑框窗口闪现。
func LaunchDaemonInBackground() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	// 为 PowerShell 单引号字符串转义反斜杠
	escapedExe := strings.ReplaceAll(exePath, `'`, `''`)
	cmdLine := fmt.Sprintf(`"%s" daemon`, escapedExe)

	// WMI SW_HIDE 启动：ShowWindow = 0 保证进程诞生时无任何控制台窗口
	psScript := fmt.Sprintf(
		`$s = ([WMIClass]'Win32_ProcessStartup').CreateInstance(); $s.ShowWindow = 0; [void]([WMIClass]'Win32_Process').Create('%s', $null, $s)`,
		cmdLine,
	)

	// 1. 通过 WMI 启动，彻底脱离任何控制台与作业树
	ps := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", psScript)
	ps.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	if err := ps.Run(); err == nil {
		return nil
	}

	// 2. 兜底：以 CREATE_NO_WINDOW 直接创建进程
	workDir := filepath.Dir(filepath.Dir(exePath))
	cmd := exec.Command(exePath, "daemon")
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000 | 0x00000200, // CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP
	}
	return cmd.Start()
}
