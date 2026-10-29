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

var (
	logOnce sync.Once
	logFile *os.File
)

var wailsNoisePrefixes = []string{"[AssetFileServerFS] Handling request"}

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

func logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)

	_, _ = fmt.Fprint(os.Stderr, line)

	if f := ensureLogFile(); f != nil {
		_, _ = f.WriteString(line)
	}
}

func ensureLogFile() *os.File {
	logOnce.Do(func() {
		base := appDataDir()
		if base == "" {
			return
		}
		if err := os.MkdirAll(base, 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(base, appName+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			logFile = f
		}
	})
	return logFile
}

func wailsLogger() *slog.Logger {
	writer := io.Writer(os.Stderr)
	if f := ensureLogFile(); f != nil {
		writer = teeWriter{stderr: os.Stderr, file: f}
	}
	return slog.New(&filterHandler{inner: slog.NewTextHandler(writer, nil), drop: wailsNoisePrefixes})
}

type teeWriter struct {
	stderr io.Writer
	file   io.Writer
}

func (t teeWriter) Write(p []byte) (int, error) {
	_, _ = t.stderr.Write(p)
	return t.file.Write(p)
}

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

func LogPath() string {
	if f := ensureLogFile(); f != nil {
		return f.Name()
	}
	if base := appDataDir(); base != "" {
		return filepath.Join(base, appName+".log")
	}
	return ""
}

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
