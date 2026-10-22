package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// 本程序以 -H windowsgui 构建，无控制台回显，故所有诊断信息同步落盘到
// %APPDATA%\language_projects\anatomymanager\anatomymanager.log，便于事后排障。
var (
	logOnce sync.Once
	logFile *os.File
)

// wailsNoisePrefixes 是 Wails 逐请求级别的噪音日志，避免淹没应用事件与错误。
var wailsNoisePrefixes = []string{"[AssetFileServerFS] Handling request"}

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

	if f := ensureLogFile(); f != nil {
		_, _ = f.WriteString(line)
	}
}

// ensureLogFile 懒初始化日志文件，失败时返回 nil（退化为仅 stderr）。
func ensureLogFile() *os.File {
	logOnce.Do(func() {
		base := appDataDir()
		if base == "" {
			return
		}
		if err := os.MkdirAll(base, 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(base, "anatomymanager.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			logFile = f
		}
	})
	return logFile
}

// wailsLogger 接管 Wails 内部日志：与应用日志同文件，过滤逐请求噪音。
func wailsLogger() *slog.Logger {
	writer := io.Writer(os.Stderr)
	if f := ensureLogFile(); f != nil {
		writer = teeWriter{stderr: os.Stderr, file: f}
	}
	return slog.New(&filterHandler{inner: slog.NewTextHandler(writer, nil), drop: wailsNoisePrefixes})
}

// teeWriter 确保 stderr 写失败（GUI 子系统无控制台）不会阻断文件落盘。
type teeWriter struct {
	stderr io.Writer
	file   io.Writer
}

func (t teeWriter) Write(p []byte) (int, error) {
	_, _ = t.stderr.Write(p)
	return t.file.Write(p)
}

// filterHandler 丢弃匹配噪音前缀的记录，其余原样交给内层 handler。
type filterHandler struct {
	inner slog.Handler
	drop  []string
}

func (h *filterHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *filterHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, prefix := range h.drop {
		if strings.HasPrefix(record.Message, prefix) {
			return nil
		}
	}
	return h.inner.Handle(ctx, record)
}

func (h *filterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &filterHandler{inner: h.inner.WithAttrs(attrs), drop: h.drop}
}

func (h *filterHandler) WithGroup(name string) slog.Handler {
	return &filterHandler{inner: h.inner.WithGroup(name), drop: h.drop}
}

// LogPath 返回日志文件绝对路径，供崩溃提示与用户反馈使用。
func LogPath() string {
	if f := ensureLogFile(); f != nil {
		return f.Name()
	}
	if base := appDataDir(); base != "" {
		return filepath.Join(base, "anatomymanager.log")
	}
	return ""
}

// Recover 作为 defer 的 panic 兜底：堆栈落盘；withDialog 为真且是 release 时弹窗提示日志位置。
func Recover(withDialog bool) {
	r := recover()
	if r == nil {
		return
	}
	logf("程序异常退出: %v\n%s", r, debug.Stack())
	if withDialog && IsRelease() {
		showFatalMessage(fmt.Sprintf("程序遇到错误已退出。\n\n日志文件：\n%s\n\n错误信息：%v", LogPath(), r))
	}
	os.Exit(1)
}
