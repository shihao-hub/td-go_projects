package app

import (
	"fmt"
	"os"
	"path/filepath"

	"mail2feishu/internal/buildinfo"
)

// DataDir 返回当前构建形态的数据目录（dev / prod 隔离）。
func DataDir() (string, error) {
	mode := "dev"
	if buildinfo.IsRelease() {
		mode = "prod"
	}
	if base := os.Getenv("APPDATA"); base != "" {
		return filepath.Join(base, "language_projects", "mail2feishu", mode), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户目录: %w", err)
	}
	return filepath.Join(home, ".language_projects", "mail2feishu", mode), nil
}

// DefaultConfigPath 返回数据目录下的 config.yaml。
func DefaultConfigPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}
