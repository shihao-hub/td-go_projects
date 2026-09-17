//go:build windows

package app

import (
	"golang.org/x/sys/windows"
)

func AlertError(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	windows.MessageBox(0, m, t, windows.MB_ICONERROR|windows.MB_OK|windows.MB_TASKMODAL)
}
