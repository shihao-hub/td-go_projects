//go:build windows

package client

import "syscall"

// procAttr Windows 下隐藏子进程控制台窗口（GUI 进程拉起 CLI 时防黑框闪烁）。
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
