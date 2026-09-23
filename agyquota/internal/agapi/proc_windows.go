//go:build windows

package agapi

import (
	"os/exec"
	"syscall"
)

// setNoWindow 在 Windows 下给子进程设置无窗口运行标志，彻底杜绝黑框闪烁与控制台重绘
func setNoWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
