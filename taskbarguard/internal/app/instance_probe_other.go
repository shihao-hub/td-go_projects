//go:build !windows

package app

// guiSingleInstanceID 非 Windows 平台沿用同一命名规则（仅供内部逻辑一致）。
func guiSingleInstanceID(release bool) string {
	id := "shihao.langproj.taskbarguard.gui"
	if !release {
		id += ".dev"
	}
	return id
}

// GUIRunning 非 Windows 平台不做互斥探测。
func GUIRunning(release bool) bool {
	return false
}
