//go:build windows

package scan

import "syscall"

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
)

// SetupConsole 在 Windows 启动时将控制台输出代码页切为 UTF-8（65001），保证中文不乱码。
func SetupConsole() {
	procSetConsoleOutputCP.Call(65001)
}
