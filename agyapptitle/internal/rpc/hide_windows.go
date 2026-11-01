//go:build windows

package rpc

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}
