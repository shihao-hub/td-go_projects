//go:build windows

package cli

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
)

// launchedFromExplorer 判断当前进程是否独占一个控制台。
//
// 双击 exe 时 Explorer 会为本进程新建控制台，进程列表里只有自己；
// 在 pwsh / cmd 里运行时进程列表包含父 shell，因此长度大于 1。
// 该判断只用于区分「双击」与「命令行调用」，不影响命令行语义。
func launchedFromExplorer() bool {
	var pids [4]uint32
	r, _, _ := procGetConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&pids[0])),
		uintptr(len(pids)),
	)
	return r == 1
}
