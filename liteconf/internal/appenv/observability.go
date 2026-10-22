package appenv

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

	"github.com/shihao-hub/liteconf/internal/version"
)

// wailsNoisePrefixes 是 Wails 逐请求级别的噪音日志，避免淹没应用事件与错误。
var wailsNoisePrefixes = []string{"[AssetFileServerFS] Handling request"}

var (
	wailsFileOnce sync.Once
	wailsFile     *os.File
)

// LogPath 返回当前通道的日志文件绝对路径。
func LogPath() string {
	return filepath.Join(DataDir(), logFileName)
}

// WailsLogger 接管 Wails 内部日志：与应用日志同一文件，过滤逐请求噪音。
func WailsLogger() *slog.Logger {
	writer := io.Writer(os.Stderr)
	if f := ensureWailsFile(); f != nil {
		writer = teeWriter{stderr: os.Stderr, file: f}
	}
	return slog.New(&filterHandler{inner: slog.NewTextHandler(writer, nil), drop: wailsNoisePrefixes})
}

// ensureWailsFile 为 Wails 日志单独打开一次追加句柄；失败时退化为仅 stderr。
func ensureWailsFile() *os.File {
	wailsFileOnce.Do(func() {
		path := LogPath()
		if path == "" {
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return
		}
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			wailsFile = f
		}
	})
	return wailsFile
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

// Recover 作为 defer 的 panic 兜底：堆栈落盘；withDialog 为真且是 release 时弹窗提示日志位置。
func Recover(withDialog bool) {
	r := recover()
	if r == nil {
		return
	}
	Logf("程序异常退出: %v\n%s", r, debug.Stack())
	if withDialog && version.IsRelease() {
		showFatalMessage(fmt.Sprintf("程序遇到错误已退出。\n\n日志文件：\n%s\n\n错误信息：%v", LogPath(), r))
	}
	os.Exit(1)
}
