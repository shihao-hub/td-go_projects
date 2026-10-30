//go:build !windows

package pool

import (
	"os"
	"os/exec"
)

// LaunchDaemonInBackground launches acp-title-proxy daemon in the background.
func LaunchDaemonInBackground() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exePath, "daemon")
	return cmd.Start()
}
