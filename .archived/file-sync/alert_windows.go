//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

func alertError(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	windows.MessageBox(0, m, t, windows.MB_ICONERROR|windows.MB_OK|windows.MB_TASKMODAL)
}
