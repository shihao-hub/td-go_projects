//go:build !windows

package app

// guiSingleInstanceID 非 Windows 平台沿用同一命名规则（仅供内部逻辑一致）。
func guiSingleInstanceID(release bool) string {
	id := "shihao.langproj.anatomymanager.gui"
	if !release {
		id += ".dev"
	}
	return id
}

// GUIRunning 非 Windows 平台暂不做互斥探测（无 Wails Windows 单实例实现），
// 恒返回 false，调用方据此走「无 GUI 运行」分支。
func GUIRunning(release bool) bool {
	return false
}
