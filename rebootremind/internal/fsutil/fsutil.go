// Package fsutil 提供原子写文件与跨进程锁等基础能力。
package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrLocked 表示锁已被其他进程持有。
var ErrLocked = errors.New("已有另一个检查进程在运行")

// WriteFileAtomic 先写同目录临时文件，再原子替换目标文件。
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// Lock 是带陈旧检测的简单互斥文件锁。
type Lock struct {
	path string
	file *os.File
}

// AcquireLock 创建锁文件；超过 staleAfter 未释放的锁视为陈旧并自动清理。
func AcquireLock(path string, staleAfter time.Duration) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			return &Lock{path: path, file: file}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		info, statErr := os.Stat(path)
		if statErr == nil && time.Since(info.ModTime()) > staleAfter {
			_ = os.Remove(path)
			continue
		}
		return nil, ErrLocked
	}
	return nil, ErrLocked
}

// Release 关闭并删除锁文件。
func (l *Lock) Release() {
	if l == nil {
		return
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	_ = os.Remove(l.path)
}
