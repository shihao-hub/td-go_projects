package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 本程序以 -H windowsgui 构建，无控制台回显，故所有诊断信息同步落盘到
// %APPDATA%\language_projects\anatomymanager\anatomymanager.log，便于事后排障。
var (
	logOnce sync.Once
	logFile *os.File
)

// appDataDir 返回本项目运行时数据目录，按构建通道隔离（dev / prod）；
// 取不到 APPDATA 时回退 ~/.language_projects/<项目名>/<通道>。
func appDataDir() string {
	channel := "prod"
	if !IsRelease() {
		channel = "dev"
	}
	if base, err := os.UserConfigDir(); err == nil && base != "" {
		return filepath.Join(base, "language_projects", appName, channel)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".language_projects", appName, channel)
	}
	return ""
}

// webviewDataPath 计算 WebView2 用户数据目录，避免缓存散落到可执行文件/工作目录。
func webviewDataPath() string {
	base := appDataDir()
	if base == "" {
		return ""
	}
	dir := filepath.Join(base, "webview2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logf("创建 WebView2 数据目录失败: %v", err)
		return ""
	}
	return dir
}

// logf 同时写入 stderr 与滚动日志文件，供无控制台构建排障。
func logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)

	_, _ = fmt.Fprint(os.Stderr, line)

	logOnce.Do(func() {
		base := appDataDir()
		if base == "" {
			return
		}
		if err := os.MkdirAll(base, 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(base, "anatomymanager.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		logFile = f
	})

	if logFile != nil {
		_, _ = logFile.WriteString(line)
	}
}
