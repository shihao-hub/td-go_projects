//go:build windows

package main

import (
	"golang.org/x/sys/windows"
	"os"
)

func init() {
	k := windows.NewLazySystemDLL("kernel32.dll")
	// ATTACH_PARENT_PROCESS = (DWORD)-1 = 0xFFFFFFFF
	r, _, _ := k.NewProc("AttachConsole").Call(uintptr(^uint32(0)))
	if r == 0 {
		return
	}
	hOut, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err == nil && hOut != 0 && hOut != windows.InvalidHandle {
		os.Stdout = os.NewFile(uintptr(hOut), "/dev/stdout")
	}
	hErr, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	if err == nil && hErr != 0 && hErr != windows.InvalidHandle {
		os.Stderr = os.NewFile(uintptr(hErr), "/dev/stderr")
	}
}
