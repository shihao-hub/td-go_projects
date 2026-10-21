//go:build windows

package appenv

import "golang.org/x/sys/windows"

// processAlive 判断 PID 对应的进程是否仍在运行。
// 用 OpenProcess(SYNCHRONIZE) 探测：僵尸 PID 与已退出进程都会失败。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	const stillActive = 259 // STILL_ACTIVE
	return code == stillActive
}
