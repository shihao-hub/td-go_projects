//go:build windows

package app

import "golang.org/x/sys/windows"

func guiSingleInstanceID(release bool) string {
	id := "shihao.langproj.nutvault.gui"
	if !release {
		id += ".dev"
	}
	return id
}

// GUIRunning 探测指定通道的 GUI 桌面壳是否在运行
func GUIRunning(release bool) bool {
	name := "wails-app-" + guiSingleInstanceID(release) + "-sim"
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(name))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}
