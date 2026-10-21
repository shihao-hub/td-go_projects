//go:build windows

package appinfo

import "golang.org/x/sys/windows"

// GUISingleInstanceID 返回 GUI 单实例互斥 ID。
//
// 这是 Wails 桌面壳的唯一真源：GUI 建锁与 where 自省探测都必须走本函数，
// 否则改名后探测会静默失效。
func GUISingleInstanceID(release bool) string {
	id := "shihao.langproj.clictl.gui"
	if !release {
		id += ".dev"
	}
	return id
}

// GUIRunning 探测指定通道的 GUI 桌面壳是否在运行。
//
// Wails v3 的单实例互斥名为 wails-app-<UniqueID>-sim，
// 用 OpenMutex 探测比按进程名匹配可靠（不受 exe 改名影响）。
func GUIRunning(release bool) bool {
	name := "wails-app-" + GUISingleInstanceID(release) + "-sim"
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(name))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}
