// Package appenv 提供运行期目录、runtime 地址发现与轻量日志。
// 数据只落 %APPDATA%\language_projects\ccquota\<dev|prod>\；
// 绝不向被读取的外部程序目录（如 ~/.cc-switch）写入任何文件。
package appenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"ccquota/internal/version"
)

const (
	appName         = "ccquota"
	runtimeFileName = "runtime.json"
	logFileName     = "ccquota.log"
)

var (
	logMu   sync.Mutex
	logFile *os.File
)

// baseDir 返回本项目数据根目录。
func baseDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "language_projects", appName)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".language_projects", appName)
	}
	return ""
}

// DataDir 返回当前通道（dev / prod）的数据目录。
func DataDir() string {
	base := baseDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, version.Channel())
}

// EnsureDataDir 创建数据目录链并返回路径。
func EnsureDataDir() (string, error) {
	dir := DataDir()
	if dir == "" {
		return "", fmt.Errorf("无法解析数据目录（缺少 APPDATA 与 HOME）")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建数据目录失败: %w", err)
	}
	return dir, nil
}

// CachePath 返回额度快照缓存文件路径。
func CachePath() (string, error) {
	dir, err := EnsureDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cards.json"), nil
}

// Runtime 描述一个运行中的 WebUI/serve 实例。
type Runtime struct {
	URL  string `json:"url"`
	PID  int    `json:"pid"`
	Mode string `json:"mode"`
}

// WriteRuntime 记录当前实例地址，供 `ccquota url` 与后续 GUI 发现。
func WriteRuntime(rt Runtime) error {
	dir, err := EnsureDataDir()
	if err != nil {
		return err
	}
	payload, err := json.MarshalIndent(rt, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(filepath.Join(dir, runtimeFileName), payload, 0o644)
}

// RemoveRuntime 仅在 PID 匹配时清理 runtime 文件，避免误删他人实例的记录。
func RemoveRuntime(pid int) {
	dir := DataDir()
	if dir == "" {
		return
	}
	path := filepath.Join(dir, runtimeFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var rt Runtime
	if json.Unmarshal(raw, &rt) == nil && rt.PID != pid {
		return
	}
	_ = os.Remove(path)
}

// DiscoverRuntime 读取当前记录的实例地址；文件不存在或内容损坏时返回 false。
func DiscoverRuntime() (Runtime, bool) {
	dir := DataDir()
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
	return rt, true
}

// Logf 追加一行带时间戳的诊断日志；失败静默，不影响主流程。
func Logf(format string, args ...any) {
	dir := DataDir()
	if dir == "" {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	if logFile == nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(dir, logFileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		logFile = f
	}
	_, _ = fmt.Fprintf(logFile, format+"\n", args...)
}
