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
	logger := log.New(io.MultiWriter(os.Stderr, file), "", log.LstdFlags)
	return logger, func() { _ = file.Close() }, nil
}
