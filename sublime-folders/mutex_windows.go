//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

const errAlreadyExists = 183

var mutexHandle uintptr

// acquireSingleInstance 用命名互斥体保证只有一个实例在跑，
// 第二个实例启动时直接退出，避免出现两个托盘图标。
func acquireSingleInstance() bool {
	name, _ := syscall.UTF16PtrFromString("Local\\sublime-folders-singleton")
	_, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW").
		Call(0, 0, uintptr(unsafe.Pointer(name)))
	return err != syscall.Errno(errAlreadyExists)
}
