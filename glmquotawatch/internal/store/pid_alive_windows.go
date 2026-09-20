//go:build windows

package store

import "golang.org/x/sys/windows"

// pidAlive 用 OpenProcess 探测进程是否存活（Windows）。
func pidAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}
