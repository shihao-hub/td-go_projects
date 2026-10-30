//go:build !windows

package pool

import (
	"os"
	"os/exec"
)

// LaunchDaemonInBackground 在后台拉起 acp-title-proxy 守护进程。
func LaunchDaemonInBackground() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exePath, "daemon")
	return cmd.Start()
}
