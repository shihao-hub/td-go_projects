// Package appenv 提供 liteconf 的运行时环境：构建通道数据目录、共享配置根目录、
// 日志落盘与服务地址发现。本包不依赖 Wails，GUI 与 CLI 两条通道共用。
package appenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shihao-hub/liteconf/internal/version"
)

const (
	appName = "liteconf"
	// runtimeFileName 记录当前通道内嵌/独立服务的实际地址，供 MCP 与 CLI 自动发现。
	runtimeFileName = "runtime.json"
	logFileName     = "liteconf.log"
)

var (
	logMu     sync.Mutex
	logFile   *os.File
	logOpened bool
	logDir    string
)

// baseDir 返回本项目数据基准目录（不含通道）：
// %APPDATA%\language_projects\liteconf，取不到 APPDATA 回退 ~/.language_projects/liteconf。
func baseDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "language_projects", appName)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".language_projects", appName)
	}
	return ""
}

// DataDir 返回当前构建通道（dev / prod）的运行时数据目录。
// 日志、窗口几何、WebView2 缓存与 runtime.json 落在该目录，通道之间互不干扰。
func DataDir() string {
	base := baseDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, version.Channel())
}

// ConfigRoot 返回配置根目录（configs/{app}/{env}.json）。
// 配置是业务数据，dev 与 prod 共用同一份，避免切通道后看不到既有配置。
func ConfigRoot() string {
	base := baseDir()
	if base == "" {
		return "configs"
	}
	return filepath.Join(base, "configs")
}

// WebviewDataPath 返回 WebView2 用户数据目录，避免缓存散落到可执行文件目录。
func WebviewDataPath() string {
	dir := DataDir()
	if dir == "" {
		return ""
	}
	webview := filepath.Join(dir, "webview2")
	if err := os.MkdirAll(webview, 0o755); err != nil {
		Logf("创建 WebView2 数据目录失败: %v", err)
		return ""
	}
	return webview
}

// Logf 同时写入 stderr 与通道日志文件，供无控制台构建排障。
func Logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)

	_, _ = fmt.Fprint(os.Stderr, line)

	dir := DataDir()
	if dir == "" {
		return
	}
	logMu.Lock()
	if !logOpened || logDir != dir {
		if logFile != nil {
			_ = logFile.Close()
			logFile = nil
		}
		if err := os.MkdirAll(dir, 0o755); err == nil {
			if f, err := os.OpenFile(filepath.Join(dir, logFileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				logFile = f
				logOpened = true
				logDir = dir
			}
		}
	}
	f := logFile
	logMu.Unlock()

	if f != nil {
		_, _ = f.WriteString(line)
	}
}

// CloseLog 关闭日志文件句柄。进程正常退出或测试清理时调用，
// 避免 Windows 上残留句柄导致临时目录无法删除。
func CloseLog() {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
	logOpened = false
	logDir = ""
}

// Runtime 描述一个已启动的 liteconf 服务实例。
// GUI 内嵌服务与 CLI serve 都会写入，后启动者覆盖，退出时按 PID 校验删除。
type Runtime struct {
	URL       string    `json:"url"`
	PID       int       `json:"pid"`
	Channel   string    `json:"channel"`
	Version   string    `json:"version"`
	Mode      string    `json:"mode"`
	StartedAt time.Time `json:"startedAt"`
}

// RuntimePath 返回当前通道 runtime.json 路径。
func RuntimePath() string {
	dir := DataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, runtimeFileName)
}

// WriteRuntime 原子写入服务地址快照（临时文件 + rename，不留半截 JSON）。
func WriteRuntime(rt Runtime) error {
	path := RuntimePath()
	if path == "" {
		return fmt.Errorf("数据目录不可用")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if rt.Channel == "" {
		rt.Channel = version.Channel()
	}
	if rt.Version == "" {
		rt.Version = version.Version
	}
	if rt.StartedAt.IsZero() {
		rt.StartedAt = time.Now()
	}
	raw, err := json.MarshalIndent(rt, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "runtime-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// ReadRuntime 读取当前通道的服务地址快照。
// 文件缺失、解析失败或进程已退出时返回 ok=false，调用方回退默认地址。
func ReadRuntime() (Runtime, bool) {
	dir := DataDir()
	if dir == "" {
		return Runtime{}, false
	}
	return readRuntimeAt(dir)
}

// DiscoverRuntime 查找正在运行的服务地址：优先当前通道，未命中再尝试另一通道。
// GUI 与 CLI 的构建通道可能不同（如 release GUI + dev CLI），跨通道发现可避免
// 「服务明明在跑却连不上」；地址本身仍由 runtime.json 的 PID 存活校验兜底。
func DiscoverRuntime() (Runtime, bool) {
	if rt, ok := ReadRuntime(); ok {
		return rt, true
	}
	base := baseDir()
	if base == "" {
		return Runtime{}, false
	}
	other := "prod"
	if version.Channel() == "prod" {
		other = "dev"
	}
	return readRuntimeAt(filepath.Join(base, other))
}

// readRuntimeAt 读取指定目录的 runtime.json 并校验进程存活。
func readRuntimeAt(dir string) (Runtime, bool) {
	if dir == "" {
		return Runtime{}, false
	}
	raw, err := os.ReadFile(filepath.Join(dir, runtimeFileName))
	if err != nil {
		return Runtime{}, false
	}
	var rt Runtime
	if err := json.Unmarshal(raw, &rt); err != nil || rt.URL == "" {
		return Runtime{}, false
	}
	if rt.PID > 0 && !processAlive(rt.PID) {
		return Runtime{}, false
	}
	return rt, true
}

// RemoveRuntime 仅在文件记录的 PID 与调用方一致时删除，避免误删其他实例的地址。
func RemoveRuntime(pid int) {
	path := RuntimePath()
	if path == "" {
		return
	}
	rt, ok := readRawRuntime(path)
	if ok && pid > 0 && rt.PID != 0 && rt.PID != pid {
		return
	}
	_ = os.Remove(path)
}

// readRawRuntime 只做文件解析，不校验进程存活（用于清理路径）。
func readRawRuntime(path string) (Runtime, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Runtime{}, false
	}
	var rt Runtime
	if err := json.Unmarshal(raw, &rt); err != nil {
		return Runtime{}, false
	}
	return rt, true
}
