//go:build windows

package app

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// ShowFatal 在 GUI 启动失败时弹出系统对话框（GUI 产物没有控制台可看）。
func ShowFatal(title, text string) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	messageBox := user32.NewProc("MessageBoxW")
	titlePtr, _ := windows.UTF16PtrFromString(title)
	textPtr, _ := windows.UTF16PtrFromString(text)
	const mbIconError = 0x00000010
	_, _, _ = messageBox.Call(0,
		uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		mbIconError,
	)
}
