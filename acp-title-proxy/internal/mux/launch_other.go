//go:build !windows

package mux

import (
	"os"
	"os/exec"
	"path/filepath"
)

func LaunchDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "share")
	cmd.Dir = filepath.Dir(exe)
	return cmd.Start()
}
