//go:build windows

package app

import "golang.org/x/sys/windows"

// guiSingleInstanceID 是 GUI 单实例互斥 ID 的唯一真源。
func guiSingleInstanceID(release bool) string {
	id := "shihao.langproj.taskbarguard.gui"
	if !release {
		id += ".dev"
	}
	return id
}

// GUIRunning 探测指定通道的 GUI 桌面壳是否在运行。
// Wails v3 的互斥名为 `wails-app-<UniqueID>-sim`，比按进程名匹配可靠。
func GUIRunning(release bool) bool {
	name := "wails-app-" + guiSingleInstanceID(release) + "-sim"
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(name))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}
