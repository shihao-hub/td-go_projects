// logx：GUI 无控制台时的最小可观测性——stderr 与数据目录日志双写。
package logx

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	// FileName 数据目录下的日志文件名。
	FileName = "termhub.log"
	// maxBytes 单文件上限，超过后保留一份 .1 轮转。
	maxBytes = 2 * 1024 * 1024
)

var (
	mu   sync.Mutex
	file *os.File
)

// Init 打开 <dataDir>/termhub.log 并返回双写 logger；stderr 写失败不影响落盘。
func Init(dataDir string) *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	closeLocked()

	if dataDir == "" {
		return slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	_ = os.MkdirAll(dataDir, 0o755)
	path := filepath.Join(dataDir, FileName)
	rotate(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	file = f
	return slog.New(slog.NewTextHandler(teeWriter{stderr: os.Stderr, file: f}, nil))
}

// Close 关闭日志文件；可重复调用。
func Close() {
	mu.Lock()
	defer mu.Unlock()
	closeLocked()
}

func closeLocked() {
	if file != nil {
		_ = file.Close()
		file = nil
	}
}

// rotate 日志超过上限时轮转为 .1，只保留一份历史。
func rotate(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxBytes {
		return
	}
	_ = os.Remove(path + ".1")
	_ = os.Rename(path, path+".1")
}

// teeWriter 先尝试 stderr（失败忽略），再写文件。
type teeWriter struct {
	stderr io.Writer
	file   io.Writer
}

func (t teeWriter) Write(p []byte) (int, error) {
	if t.stderr != nil {
		_, _ = t.stderr.Write(p)
	}
	return t.file.Write(p)
}
