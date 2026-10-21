//go:build !windows

package instance

// GUIRunning 非 Windows 平台暂不做互斥探测，恒返回 false，
// 调用方据此走「无 GUI 运行」分支。
func GUIRunning(release bool) bool {
	return false
}
