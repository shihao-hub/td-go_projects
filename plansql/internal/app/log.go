package app

import (
	"fmt"
	"os"
	"path/filepath"
	"plansql/internal/daemon"
	"sync"
	"time"
)

// 本程序 GUI 通道以 -H windowsgui 构建，无控制台回显，故所有诊断信息同步落盘到
// %APPDATA%\language_projects\plansql\<dev|prod>\plansql.log，便于事后排障。
var (
	logOnce sync.Once
	logFile *os.File
)

const appName = "plansql"

// appDataDir 返回本项目当前通道的运行时数据目录。
// 与 daemon 的地址文件、CLI where 自省共用同一真源，避免两份路径推导。
func appDataDir() string {
	dir, err := daemon.GetDataDir()
	if err != nil {
		return ""
	}
	return dir
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
		f, err := os.OpenFile(filepath.Join(base, appName+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		logFile = f
	})

	if logFile != nil {
		_, _ = logFile.WriteString(line)
	}
}
