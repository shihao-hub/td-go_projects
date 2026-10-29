package app

import (
	"path/filepath"
)

// DataDir 返回当前构建通道（dev / prod）的运行时数据目录。
func DataDir() string {
	return appDataDir()
}

// DefaultDBPath 返回默认 SQLite 路径
func DefaultDBPath() string {
	dir := appDataDir()
	if dir == "" {
		return "nutvault.db"
	}
	return filepath.Join(dir, "nutvault.db")
}
