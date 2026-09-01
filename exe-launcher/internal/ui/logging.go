package ui

import (
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
)

// 日志落在 %AppData%\exe-launcher\exe-launcher.log，追加写。
// GUI 程序没有控制台，panic / 关键动作都必须落到这个文件才可定位。

func InitLogging() {
	base, err := os.UserConfigDir()
	if err != nil {
		return
	}
	dir := filepath.Join(base, "exe-launcher")
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "exe-launcher.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags)
}

func LogPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "exe-launcher", "exe-launcher.log")
}

// recoverLog 捕获 wndproc 回调里的 panic。panic 穿过 NewCallback 边界会直接 exit(2)，
// windowsgui 下无任何输出，必须在这里拦下写日志。
func recoverLog(what string) {
	if r := recover(); r != nil {
		log.Printf("PANIC in %s: %v\n%s", what, r, debug.Stack())
	}
}

func LogFatal(format string, args ...any) {
	log.Printf("FATAL "+format, args...)
}
