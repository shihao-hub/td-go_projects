package model

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// config 落盘于 %AppData%\exe-launcher\config.json。
// valid 是运行时状态（os.Stat 重算），不落盘。
type Config struct {
	Entries     []Entry `json:"entries"`
	LastScanDir string  `json:"last_scan_dir"`
}

func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "exe-launcher", "config.json"), nil
}

// LoadConfig 任何失败（无文件/损坏）都按空配置处理。
func LoadConfig() *Config {
	c := &Config{}
	p, err := configPath()
	if err != nil {
		return c
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, c)
	return c
}

func SaveConfig(c *Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
