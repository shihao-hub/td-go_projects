//go:build windows

// Package sysactions 封装需要操作系统副作用的动作。
package sysactions

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// Restart 调用 shutdown /r /t 0 立即重启系统。
func Restart() error {
	cmd := exec.Command("shutdown.exe", "/r", "/t", "0")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("shutdown /r 失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
