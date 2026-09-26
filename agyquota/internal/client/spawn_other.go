//go:build !windows

package client

import (
	"os/exec"
	"syscall"
)

// configureDetached 让 daemon 脱离客户端会话（新会话首进程），
// 关闭启动它的终端不影响 daemon。
func configureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
