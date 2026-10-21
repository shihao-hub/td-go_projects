// Package appinfo 提供与桌面壳无关的构建信息、数据目录与诊断日志。
//
// 该包刻意不依赖 Wails：CLI 通道（-tags cli）与 internal/api 都会引用它，
// 保证 console 产物不链接桌面壳依赖。
package appinfo

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AppName 项目名，同时是数据目录与单实例标识的组成片段。
const AppName = "clictl"

var (
	// Version 编译期通过 -ldflags "-X clictl/internal/appinfo.Version=..." 注入
	Version = "dev"
	// BuildMode 编译期通过 -ldflags "-X clictl/internal/appinfo.BuildMode=release" 注入
	BuildMode = "dev"
)

// IsRelease 返回当前二进制是否为正式发行版。
// 单一真源：只认编译期注入的 BuildMode，不根据版本文本猜测。
func IsRelease() bool {
	return BuildMode == "release"
}

// Channel 返回当前构建通道名：release → prod，dev → dev。
func Channel() string {
	if IsRelease() {
		return "prod"
	}
	return "dev"
}

var (
	logOnce sync.Once
	logFile *os.File
)

// DataDir 返回当前通道的运行时数据目录（dev / prod 隔离）。
// 优先 %APPDATA%\language_projects\clictl\<通道>，回退 ~/.language_projects/clictl/<通道>。
func DataDir() string {
	if base, err := os.UserConfigDir(); err == nil && base != "" {
		return filepath.Join(base, "language_projects", AppName, Channel())
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".language_projects", AppName, Channel())
	}
	return ""
}

// LegacyDBPath 双通道改造前的固定库路径，仅用于一次性迁移。
func LegacyDBPath() string {
	if base, err := os.UserConfigDir(); err == nil && base != "" {
		return filepath.Join(base, "language_projects", AppName, "clictl.db")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".language_projects", AppName, "clictl.db")
	}
	return ""
}

// DefaultDBPath 返回当前通道的 SQLite 路径；目标缺失时从旧库复制
// （含 -wal / -shm），旧文件保留不删。
func DefaultDBPath() string {
	dir := DataDir()
	target := filepath.Join(dir, "clictl.db")
	migrateLegacyDB(target)
	return target
}

// WebviewDataPath 计算 WebView2 用户数据目录，避免缓存散落到可执行文件目录。
func WebviewDataPath() string {
	dir := filepath.Join(DataDir(), "webview2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		Logf("创建 WebView2 数据目录失败: %v", err)
		return ""
	}
	return dir
}

// LogPath 返回诊断日志路径（与库同通道目录）。
func LogPath() string {
	return filepath.Join(DataDir(), AppName+".log")
}

// Logf 同时写 stderr 与日志文件；GUI 产物无控制台时只能靠落盘日志排障。
func Logf(format string, args ...any) {
	line := fmt.Sprintf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	_, _ = fmt.Fprint(os.Stderr, line)

	logOnce.Do(func() {
		dir := DataDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(dir, AppName+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		logFile = f
	})
	if logFile != nil {
		_, _ = logFile.WriteString(line)
	}
}
