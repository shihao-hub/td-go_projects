//go:build windows && !cli

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// init 让 GUI 子系统（-H windowsgui）的二进制仍能在终端里跑 CLI。
//
// 两条硬约束：
//  1. 父进程已给到可用标准句柄（管道 / `> file` 重定向）时必须沿用，不能抢父控制台，
//     否则 `anatomymanager.exe list --json > a.json` 与 `| jq` 会拿到空内容；
//     AttachConsole 会把标准句柄重置为控制台句柄，所以有效句柄要先存后还原。
//  2. 仅当输出句柄是靠 AttachConsole 抢来的父控制台时，才先补一个换行：
//     PowerShell 对 GUI 子系统进程不等待，prompt 早已渲染完毕，后续输出会从
//     prompt 所在行的光标处开始写，视觉上会黏成一行。
//
// 注：终端 / 脚本 / Agent 场景应优先使用 console 通道产物（anatomymanager-cli.exe），
// 它由 shell 正常等待，本文件的全部补偿机制都与它无关。
func init() {
	hOut, errOut := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	hErr, errErr := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	outOK := usable(errOut, hOut)
	errOK := usable(errErr, hErr)
	if outOK && errOK {
		return
	}

	// ATTACH_PARENT_PROCESS = (DWORD)-1 = 0xFFFFFFFF
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	if r, _, _ := kernel32.NewProc("AttachConsole").Call(uintptr(^uint32(0))); r == 0 {
		return
	}

	// 有效句柄优先还原；无效者才改用 Attach 后拿到的控制台句柄。
	switch {
	case outOK:
		os.Stdout = os.NewFile(uintptr(hOut), "/dev/stdout")
	default:
		if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); usable(err, h) {
			os.Stdout = os.NewFile(uintptr(h), "/dev/stdout")
			_, _ = fmt.Fprint(os.Stdout, "\r\n")
		}
	}
	if errOK {
		os.Stderr = os.NewFile(uintptr(hErr), "/dev/stderr")
	} else if h, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE); usable(err, h) {
		os.Stderr = os.NewFile(uintptr(h), "/dev/stderr")
	}
}

func usable(err error, h windows.Handle) bool {
	return err == nil && h != 0 && h != windows.InvalidHandle
}
