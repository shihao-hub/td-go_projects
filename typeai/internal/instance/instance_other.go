//go:build !windows

package instance

// GUIRunning 非 Windows 平台暂不做互斥探测，恒返回 false。
func GUIRunning(release bool) bool {
	return false
}
