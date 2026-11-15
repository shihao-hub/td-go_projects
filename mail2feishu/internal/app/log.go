package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// NewLogger 创建同时写 stderr 与数据目录文件的日志器。
// GUI 产物没有控制台，文件日志是唯一可回溯的诊断通道。
func NewLogger(dir string) (*log.Logger, func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "mail2feishu.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("打开日志文件失败: %w", err)
	}
	logger := log.New(bestEffortWriter{os.Stderr, file}, "", log.LstdFlags)
	return logger, func() { _ = file.Close() }, nil
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
