//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

// procTree 非 Windows 用进程组管理：子进程 Setpgid 自成组长，
// kill 用负 PID 对整组发信号；父进程退出后由 init 接管孤儿，
// 无 Windows Job Object 的"宿主崩溃兜底回收"等价物（可叠加 PDEATHSIG，暂不引入）。
type procTree struct {
	pid int
}

func newProcTree() (*procTree, error) { return &procTree{}, nil }

// setup 在 Start 前配置子进程属性（独立进程组）
func (p *procTree) setup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// attach 在 cmd.Start 成功后调用：记录组长 PID
func (p *procTree) attach(cmd *exec.Cmd) error {
	p.pid = cmd.Process.Pid
	return nil
}

// kill 对整个进程组发 SIGKILL
func (p *procTree) kill() {
	if p.pid > 0 {
		_ = syscall.Kill(-p.pid, syscall.SIGKILL)
	}
}

func (p *procTree) close() {}
