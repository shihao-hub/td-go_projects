//go:build !windows

package agapi

import "os/exec"

func setNoWindow(cmd *exec.Cmd) {}
