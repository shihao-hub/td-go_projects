//go:build windows

package pool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// LaunchDaemonInBackground launches acp-title-proxy daemon silently without a console window.
// It prioritizes WMI process creation (Parent: WmiPrvSE.exe) to completely decouple
// the daemon from Zed's process tree and Job Object.
func LaunchDaemonInBackground() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	cmdLine := fmt.Sprintf(`"%s" daemon`, exePath)
	psCmd := fmt.Sprintf(`Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine = '%s'}`, cmdLine)

	// 1. Launch via WMI for complete detachment from any console/job tree
	ps := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", psCmd)
	ps.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if err := ps.Run(); err == nil {
		return nil
	}

	// 2. Fallback to direct process spawn
	workDir := filepath.Dir(filepath.Dir(exePath))
	cmd := exec.Command(exePath, "daemon")
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000 | 0x00000200, // CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP
	}
	return cmd.Start()
}
