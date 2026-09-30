//go:build windows

package daemon

import (
	"os/exec"
	"syscall"
)

func setSysProcAttr(cmd *exec.Cmd) {
	// 0x00000008 = DETACHED_PROCESS, 0x00000200 = CREATE_NEW_PROCESS_GROUP
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008,
	}
}
