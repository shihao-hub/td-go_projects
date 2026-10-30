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

// LaunchDaemonInBackground launches acp-title-proxy daemon silently without a console window.
// It prioritizes WMI process creation (Parent: WmiPrvSE.exe) with SW_HIDE (ShowWindow = 0)
// to completely decouple the daemon from Zed's process tree and prevent any black console window from popping up.
func LaunchDaemonInBackground() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	// Escape backslashes for PowerShell single quotes
	escapedExe := strings.ReplaceAll(exePath, `'`, `''`)
	cmdLine := fmt.Sprintf(`"%s" daemon`, escapedExe)

	// WMI SW_HIDE startup: ShowWindow = 0 guarantees zero console window on birth
	psScript := fmt.Sprintf(
		`$s = ([WMIClass]'Win32_ProcessStartup').CreateInstance(); $s.ShowWindow = 0; [void]([WMIClass]'Win32_Process').Create('%s', $null, $s)`,
		cmdLine,
	)

	// 1. Launch via WMI for complete detachment from any console/job tree
	ps := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", psScript)
	ps.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	if err := ps.Run(); err == nil {
		return nil
	}

	// 2. Fallback to direct process spawn with CREATE_NO_WINDOW
	workDir := filepath.Dir(filepath.Dir(exePath))
	cmd := exec.Command(exePath, "daemon")
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000 | 0x00000200, // CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP
	}
	return cmd.Start()
}
