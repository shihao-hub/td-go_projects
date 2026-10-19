//go:build windows

package main

import (
	"os/exec"
	"strconv"
	"syscall"
)

// CREATE_NO_WINDOW：无控制台子进程，杜绝终端黑框闪烁。
const createNoWindowFlag = 0x08000000

// newHiddenCommand 构造一个静默运行的子进程命令（Windows）。
func newHiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindowFlag,
	}
	return cmd
}

// killTree 终止指定进程及其整棵子树（Windows）。
func killTree(pid int) {
	killCmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	killCmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindowFlag,
	}
	if err := killCmd.Run(); err != nil {
		logf("回收进程树失败 pid=%d: %v", pid, err)
	}
}
