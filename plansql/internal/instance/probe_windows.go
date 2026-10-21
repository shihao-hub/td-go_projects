//go:build windows

package instance

import "golang.org/x/sys/windows"

// GUIRunning 探测指定通道的 GUI 桌面壳是否在运行。
//
// 依据 Wails v3 的单实例实现：互斥名为 `wails-app-<UniqueID>-sim`，
// 用 OpenMutex 探测比按进程名匹配可靠（不受 exe 改名、僵尸进程名影响）。
func GUIRunning(release bool) bool {
	name := "wails-app-" + GUIID(release) + "-sim"
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(name))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}
