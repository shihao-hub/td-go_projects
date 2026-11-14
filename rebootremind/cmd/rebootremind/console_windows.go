//go:build windows

// console_windows.go：GUI 子系统 exe 的终端输出契约恢复。
//
// 本 exe 以 -H=windowsgui 链接为 GUI 子系统，计划任务、双击等无控制台宿主
// 拉起时 Windows 不再为其强制新建可见控制台（根治告警巡检时弹出终端黑窗）；
// 代价是从终端裸跑时标准句柄为空，status/config 等人读输出不可见。
// attachParentConsole 在 main 启动早期把控制台挂回父进程终端并重绑标准流，
// 让 GUI 子系统形态同时满足「后台无窗口」与「终端有输出」（CLI 标准 §4.1）。

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

var (
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole  = kernel32.NewProc("AttachConsole")
)

const attachParentProcess = 0xFFFFFFFF // ATTACH_PARENT_PROCESS

func attachParentConsole() {
	attached, _, _ := procAttachConsole.Call(attachParentProcess)
	if attached == 0 {
		// 无父控制台（计划任务、双击、服务宿主）或已附着控制台：静默保持
		// 无窗口。绝不 AllocConsole——新建可见控制台正是本次修复的弹窗根源。
		return
	}
	rebindConsoleStream(windows.STD_OUTPUT_HANDLE, &os.Stdout)
	rebindConsoleStream(windows.STD_ERROR_HANDLE, &os.Stderr)
}

// rebindConsoleStream 把失效的标准流重绑到当前控制台。句柄已有效时
// （输出被重定向到文件/管道，或系统已注入句柄）保持原状，不破坏重定向契约。
func rebindConsoleStream(stdHandle uint32, target **os.File) {
	handle, err := windows.GetStdHandle(stdHandle)
	if err == nil && handle != windows.InvalidHandle && handle != 0 {
		*target = os.NewFile(uintptr(handle), "CONOUT$")
		return
	}
	// GUI 子系统启动时标准句柄为空，AttachConsole 也不会自动设置，
	// 直接打开控制台屏幕缓冲区兜底；打不开则维持静默无输出。
	file, fileErr := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if fileErr != nil {
		return
	}
	*target = file
}
