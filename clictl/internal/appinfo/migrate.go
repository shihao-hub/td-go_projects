package appinfo

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// migrateLegacyDB 仅在目标不存在时复制旧库（含 wal/shm），不删除旧文件。
func migrateLegacyDB(target string) {
	legacy := LegacyDBPath()
	if legacy == target {
		return
	}
	if _, err := os.Stat(target); err == nil {
		return
	}
	if _, err := os.Stat(legacy); err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		Logf("创建数据目录失败: %v", err)
		return
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := copyFile(legacy+suffix, target+suffix); err != nil {
			if suffix == "" {
				Logf("迁移旧数据库失败（继续保留旧库）: %v", err)
				return
			}
		}
	}
	Logf("已迁移旧数据库 %s -> %s（旧文件保留）", legacy, target)
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
