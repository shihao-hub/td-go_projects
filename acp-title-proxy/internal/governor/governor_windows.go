//go:build windows

package governor

import (
	"errors"
	"golang.org/x/sys/windows"
)

type winProcessLock struct {
	handle windows.Handle
}

func defaultProcessLock() crossProcessLock {
	namePtr, err := windows.UTF16PtrFromString("Local\\acp-title-proxy-governor-mutex")
	if err != nil {
		return &fallbackLocalLock{}
	}
	h, err := windows.CreateMutex(nil, false, namePtr)
	if err != nil || h == 0 {
		return &fallbackLocalLock{}
	}
	return &winProcessLock{handle: h}
}

func (l *winProcessLock) Lock() error {
	if l.handle == 0 {
		return nil
	}
	// 5 秒超时保护，防止异常崩溃进程持有锁导致死锁
	event, err := windows.WaitForSingleObject(l.handle, 5000)
	if err != nil {
		return err
	}
	if event == uint32(windows.WAIT_TIMEOUT) {
		return errors.New("governor cross-process lock timed out (5s)")
	}
	if event != windows.WAIT_OBJECT_0 && event != 0x00000080 { // WAIT_OBJECT_0 or WAIT_ABANDONED
		return errors.New("governor cross-process lock failed")
	}
	return nil
}

func (l *winProcessLock) Unlock() {
	if l.handle != 0 {
		_ = windows.ReleaseMutex(l.handle)
	}
}

func defaultIsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	// STILL_ACTIVE = 259
	return code == 259
}

type fallbackLocalLock struct{}

func (f *fallbackLocalLock) Lock() error { return nil }
func (f *fallbackLocalLock) Unlock()     {}
