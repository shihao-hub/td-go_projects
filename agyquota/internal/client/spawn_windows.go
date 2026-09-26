//go:build windows

package client

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// configureDetached 让 daemon 完全脱离客户端进程：
// DETACHED_PROCESS（无控制台、脱离父子树）+ 新进程组 + 不显示窗口，
// 关闭启动它的终端不影响 daemon。
func configureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}
