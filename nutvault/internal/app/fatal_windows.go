package app

import "golang.org/x/sys/windows"

func showFatalMessage(body string) {
	title, err := windows.UTF16PtrFromString("NutVault 启动失败")
	if err != nil {
		return
	}
	text, err := windows.UTF16PtrFromString(body)
	if err != nil {
		return
	}
	_, _ = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR)
}
