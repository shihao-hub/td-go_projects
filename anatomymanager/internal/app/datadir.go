package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DataDir 返回当前构建通道（dev / prod）的运行时数据目录。
func DataDir() string {
	return appDataDir()
}

// DefaultDBPath 返回默认 SQLite 路径；首次启动时把旧版 ~/.anatomy/anatomy.db
// 一次性复制到规范数据目录（旧文件保留，不删除）。
func DefaultDBPath() string {
	dir := appDataDir()
	if dir == "" {
		return "anatomy.db"
	}
	target := filepath.Join(dir, "anatomy.db")
	migrateLegacyDB(target)
	return target
}

// legacyDBPath 旧版固定路径，仅用于一次性迁移。
func legacyDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".anatomy", "anatomy.db")
}

// migrateLegacyDB 仅在目标不存在时复制旧库（含 wal/shm），不删除旧文件。
func migrateLegacyDB(target string) {
	legacy := legacyDBPath()
	if legacy == "" || legacy == target {
		return
	}
	if _, err := os.Stat(target); err == nil {
		return
	}
	if _, err := os.Stat(legacy); err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		logf("创建数据目录失败: %v", err)
		return
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := copyFile(legacy+suffix, target+suffix); err != nil {
			if suffix == "" {
				logf("迁移旧数据库失败（继续保留旧库）: %v", err)
				return
			}
		}
	}
	logf("已迁移旧数据库 %s -> %s（旧文件保留）", legacy, target)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	return out.Sync()
}
