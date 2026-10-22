package main

import (
	"golang.org/x/sys/windows"
)

// showFatalMessage 在 release 通道无控制台时给出可见反馈。
func showFatalMessage(body string) {
	title, err := windows.UTF16PtrFromString("WinStartupManager 启动失败")
	if err != nil {
		return
	}
	text, err := windows.UTF16PtrFromString(body)
	if err != nil {
		return
	}
	_, _ = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR)
}
