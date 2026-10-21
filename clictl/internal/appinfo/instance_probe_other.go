//go:build !windows

package appinfo

// GUISingleInstanceID 非 Windows 平台沿用同一命名规则（仅供内部逻辑一致）。
func GUISingleInstanceID(release bool) string {
	id := "shihao.langproj.clictl.gui"
	if !release {
		id += ".dev"
	}
	return id
}

// GUIRunning 非 Windows 平台不做互斥探测，恒返回 false。
func GUIRunning(release bool) bool { return false }
