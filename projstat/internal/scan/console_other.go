//go:build !windows

package scan

// SetupConsole 在非 Windows 平台无需处理代码页，空实现。
func SetupConsole() {}
