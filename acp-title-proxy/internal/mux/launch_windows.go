//go:build windows

package mux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// LaunchDaemon 静默拉起 share daemon，避免控制台窗口闪现。
func LaunchDaemon() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	escaped := strings.ReplaceAll(exePath, `'`, `''`)
	cmdLine := fmt.Sprintf(`"%s" share`, escaped)
	psScript := fmt.Sprintf(
		`$s = ([WMIClass]'Win32_ProcessStartup').CreateInstance(); $s.ShowWindow = 0; [void]([WMIClass]'Win32_Process').Create('%s', $null, $s)`,
		cmdLine,
	)

	ps := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", psScript)
	ps.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	if err := ps.Run(); err == nil {
		return nil
	}

	cmd := exec.Command(exePath, "share")
	cmd.Dir = filepath.Dir(exePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000 | 0x00000200, // CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP
	}
	return cmd.Start()
}
