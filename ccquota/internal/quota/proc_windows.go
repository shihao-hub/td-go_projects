//go:build windows

package quota

import (
	"os/exec"
	"syscall"
)

// hideWindow 抑制控制台子进程的闪窗（agy 是 console 程序）。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
