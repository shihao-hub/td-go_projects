package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	logFileName = "mail2feishu.log"
	logMaxBytes = 5 << 20
	logBackups  = 3
)

// NewLogger 创建同时写 stderr 与数据目录文件的日志器。
// GUI 产物没有控制台，文件日志是唯一可回溯的诊断通道；常驻运行必须按大小轮转，
// 否则日志会无限增长。
func NewLogger(dir string) (*log.Logger, func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	writer, err := openRotatingFile(filepath.Join(dir, logFileName), logMaxBytes, logBackups)
	if err != nil {
		return nil, nil, err
	}
	logger := log.New(bestEffortWriter{os.Stderr, writer}, "", log.LstdFlags)
	return logger, func() { _ = writer.Close() }, nil
}

// bestEffortWriter 把日志同时写到多个目标，任一目标失败不影响其它目标。
// GUI 子系统下 stderr 是无效句柄，若用 io.MultiWriter，stderr 写失败会
// 直接短路，导致文件日志一并丢失。
type bestEffortWriter []io.Writer

func (writers bestEffortWriter) Write(p []byte) (int, error) {
	for _, writer := range writers {
		_, _ = writer.Write(p)
	}
	return len(p), nil
}

// rotatingFile 是按大小轮转的单文件写入器：写满后依次滚动 .1 / .2 / .3。
type rotatingFile struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	file     *os.File
	size     int64
}

func openRotatingFile(path string, maxBytes int64, backups int) (*rotatingFile, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败: %w", err)
	}
	size := int64(0)
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}
	return &rotatingFile{path: path, maxBytes: maxBytes, backups: backups, file: file, size: size}, nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return 0, os.ErrClosed
	}
	if r.maxBytes > 0 && r.size+int64(len(p)) > r.maxBytes {
		if err := r.rotateLocked(); err != nil {
			return 0, err
		}
	}
	written, err := r.file.Write(p)
	r.size += int64(written)
	return written, err
}

func (r *rotatingFile) rotateLocked() error {
	if err := r.file.Close(); err != nil {
		return fmt.Errorf("关闭日志文件失败: %w", err)
	}
	r.file = nil
	if r.backups > 0 {
		_ = os.Remove(fmt.Sprintf("%s.%d", r.path, r.backups))
	}
	for i := r.backups - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", r.path, i), fmt.Sprintf("%s.%d", r.path, i+1))
	}
	_ = os.Rename(r.path, r.path+".1")

	file, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("轮转后重新打开日志失败: %w", err)
	}
	r.file = file
	r.size = 0
	return nil
}

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}
