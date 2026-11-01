//go:build !windows

package rpc

import "os/exec"

func prepareCommand(cmd *exec.Cmd) {}
