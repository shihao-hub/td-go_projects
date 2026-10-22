package logx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// FileName 是落盘日志文件名，位于 <appDataDir> 下。
	FileName = "winstartupmanager.log"
	// MaxBytes 是单文件上限，超过后启动时轮转为 .1.log（只保留一份）。
	MaxBytes int64 = 2 << 20
)

var (
	mu           sync.Mutex
	logger       *slog.Logger
	systemLogger *slog.Logger
	logFile      *os.File
)

// systemNoisePrefixes 是 Wails 逐请求级别的噪音日志，避免淹没应用事件与错误。
var systemNoisePrefixes = []string{"[AssetFileServerFS] Handling request"}

// Init 初始化日志：始终双写 stderr + <dataDir>/winstartupmanager.log。
// dataDir 为空或不可写时退化为仅 stderr（dev 可见，release 无控制台则丢弃）。
// 幂等：重复调用返回同一实例。
func Init(dataDir string) *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	if logger != nil {
		return logger
	}

	writer := io.Writer(os.Stderr)
	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0755); err == nil {
			path := filepath.Join(dataDir, FileName)
			_ = rotate(path, MaxBytes)
			if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
				logFile = f
				writer = teeWriter{stderr: os.Stderr, file: f}
			}
		}
	}

	logger = slog.New(newHandler(writer))
	systemLogger = slog.New(&filterHandler{inner: newHandler(writer), drop: systemNoisePrefixes})
	return logger
}

func newHandler(writer io.Writer) slog.Handler {
	return slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.String("time", attr.Value.Time().Format("2006-01-02 15:04:05"))
			}
			return attr
		},
	})
}

// SystemLogger 供 Wails 内部日志使用：同一文件，但过滤逐请求噪音。
func SystemLogger() *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	if systemLogger == nil {
		return Logger()
	}
	return systemLogger
}

// Logger 返回已初始化的 logger；未初始化时返回仅 stderr 的兜底实例。
func Logger() *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return logger
}

func Infof(format string, args ...any)  { Logger().Info(fmt.Sprintf(format, args...)) }
func Warnf(format string, args ...any)  { Logger().Warn(fmt.Sprintf(format, args...)) }
func Errorf(format string, args ...any) { Logger().Error(fmt.Sprintf(format, args...)) }

// Close 关闭日志文件句柄；之后可再次 Init 重建。
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
	logger = nil
	systemLogger = nil
}

// rotate 在文件超过上限时重命名为 .1.log，仅保留一份历史。
func rotate(path string, limit int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if info.Size() < limit {
		return nil
	}
	backup := path + ".1"
	_ = os.Remove(backup)
	return os.Rename(path, backup)
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
