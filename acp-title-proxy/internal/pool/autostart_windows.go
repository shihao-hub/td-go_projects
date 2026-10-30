//go:build windows

package pool

import (
	"os"
	"os/exec"
	"syscall"
)

// LaunchDaemonInBackground launches acp-title-proxy daemon silently without a console window.
func LaunchDaemonInBackground() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exePath, "daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd.Start()
}
