package runtime

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"mail2feishu/internal/audit"
)

// Records 返回最近的审计事件（最新在前）。
func (r *Runtime) Records(limit int) ([]audit.Event, error) {
	events, err := audit.ReadTail(r.opts.DataDir, limit)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, nil
}

// LogTail 返回运行日志最后 lines 行。
func (r *Runtime) LogTail(lines int) ([]string, error) {
	if lines <= 0 {
		return nil, nil
	}
	return tailFile(filepath.Join(r.opts.DataDir, "mail2feishu.log"), lines)
}

// tailFile 只读取文件末尾最多 512 KiB，避免把整份日志载入内存。
func tailFile(path string, lines int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("打开日志文件失败: %w", err)
	}
	defer file.Close()

	const maxTailBytes = 512 << 10
	if info, err := file.Stat(); err == nil && info.Size() > maxTailBytes {
		if _, err := file.Seek(-maxTailBytes, io.SeekEnd); err != nil {
			return nil, fmt.Errorf("定位日志尾部失败: %w", err)
		}
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	result := make([]string, 0, lines)
	for scanner.Scan() {
		result = append(result, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取日志失败: %w", err)
	}
	if len(result) > lines {
		result = result[len(result)-lines:]
	}
	return result, nil
}
