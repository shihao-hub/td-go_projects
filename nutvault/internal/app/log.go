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

const appName = "nutvault"

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

	logOnce.Do(func() {
		base := appDataDir()
		if base == "" {
			return
		}
		if err := os.MkdirAll(base, 0o755); err != nil {
			return
		}
		p := filepath.Join(base, appName+".log")
		rotateLegacyLog(p)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			logFile = f
			logHeader(f)
		}
	})

	if logFile != nil {
		_, _ = logFile.WriteString(line)
	}
	_, _ = os.Stderr.WriteString(line)
}

func rotateLegacyLog(p string) {
	fi, err := os.Stat(p)
	if err != nil || fi.Size() < 10*1024*1024 {
		return
	}
	backup := p + ".1"
	_ = os.Remove(backup)
	_ = os.Rename(p, backup)
}

func logHeader(w io.Writer) {
	fmt.Fprintf(w, "=== %s %s [%s] PID=%d Started ===\n",
		appName, Version, Channel(), os.Getpid())
}

type wailsBridgeLogger struct{}

func wailsLogger() *slog.Logger {
	return slog.New(&wailsLogHandler{})
}

type wailsLogHandler struct{}

func (h *wailsLogHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *wailsLogHandler) Handle(_ context.Context, r slog.Record) error {
	msg := r.Message
	for _, p := range wailsNoisePrefixes {
		if strings.HasPrefix(msg, p) {
			return nil
		}
	}
	logf("[%s] %s", r.Level, msg)
	return nil
}

func (h *wailsLogHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *wailsLogHandler) WithGroup(_ string) slog.Handler {
	return h
}

// LogPanic 捕获崩溃并落盘
func LogPanic(r any) {
	if r == nil {
		return
	}
	stack := string(debug.Stack())
	logf("FATAL PANIC: %v\n%s", r, stack)
	if base := appDataDir(); base != "" {
		crashPath := filepath.Join(base, "crash.log")
		_ = os.WriteFile(crashPath, []byte(fmt.Sprintf("%s PANIC: %v\n%s", time.Now().Format(time.RFC3339), r, stack)), 0o644)
	}
}
