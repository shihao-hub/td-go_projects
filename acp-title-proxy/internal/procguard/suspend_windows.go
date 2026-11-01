//go:build windows

package procguard

import (
	"golang.org/x/sys/windows"
)

var (
	modntdll             = windows.NewLazySystemDLL("ntdll.dll")
	procNtSuspendProcess = modntdll.NewProc("NtSuspendProcess")
	procNtResumeProcess  = modntdll.NewProc("NtResumeProcess")

	modpsapi              = windows.NewLazySystemDLL("psapi.dll")
	procEmptyWorkingSet   = modpsapi.NewProc("EmptyWorkingSet")
)

// SuspendProcess 挂起目标进程所有线程，并清空工作集将物理内存交还给操作系统。
func SuspendProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_SUSPEND_RESUME|windows.PROCESS_SET_QUOTA|windows.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)

	// 1. 挂起进程内所有线程
	r1, _, e1 := procNtSuspendProcess.Call(uintptr(h))
	if r1 != 0 {
		return e1
	}

	// 2. 裁减进程工作集，迫使 Windows 回收其物理内存占用
	_, _, _ = procEmptyWorkingSet.Call(uintptr(h))
	return nil
}

// ResumeProcess 唤醒被挂起的进程。
func ResumeProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_SUSPEND_RESUME, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)

	r1, _, e1 := procNtResumeProcess.Call(uintptr(h))
	if r1 != 0 {
		return e1
	}
	return nil
}
