package main

import (
	"os"
	"path/filepath"
)

// hashCachePath 返回全局共享的哈希缓存路径（方案 A：单文件，跨任务共享）。
func hashCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".file-sync", "hash-cache.gob"), nil
}
