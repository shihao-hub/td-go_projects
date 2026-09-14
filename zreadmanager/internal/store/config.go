// Package store 管理 zreadmanager 的本地配置（%APPDATA%\language_projects\zreadmanager\config.json）。
// 目前只记 last_dir：start 不带 --dir 时回退到上次工作区。
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config 本地配置
type Config struct {
	LastDir string `json:"last_dir"`
}

func configPath() (string, error) {
	if appData := os.Getenv("AppData"); appData != "" {
		return filepath.Join(appData, "language_projects", "zreadmanager", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".language_projects", "zreadmanager", "config.json"), nil
}

// Load 读配置（无文件/损坏都返回零值，不报错）
func Load() Config {
	p, err := configPath()
	if err != nil {
		return Config{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Config{}
	}
	var c Config
	if json.Unmarshal(data, &c) != nil {
		return Config{}
	}
	return c
}

// Save 写配置
func Save(c Config) error {
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
