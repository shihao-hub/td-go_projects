//go:build !windows

package main

import (
	"os"
	"os/exec"
)

// newHiddenCommand 在非 Windows 平台无需隐藏控制台窗口。
func newHiddenCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// killTree 在非 Windows 平台仅能终止直接子进程。
func killTree(pid int) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		logf("回收进程失败 pid=%d: %v", pid, err)
		return
	}
	if err := proc.Kill(); err != nil {
		logf("回收进程失败 pid=%d: %v", pid, err)
	}
}
