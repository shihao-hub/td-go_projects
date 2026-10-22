package main

import "golang.org/x/sys/windows"

// showFatalMessage 在无控制台的 GUI 产物中给出可见反馈。
func showFatalMessage(body string) {
	title, err := windows.UTF16PtrFromString("docsearch 启动失败")
	if err != nil {
		return
	}
	text, err := windows.UTF16PtrFromString(body)
	if err != nil {
		return
	}
	_, _ = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR)
}
