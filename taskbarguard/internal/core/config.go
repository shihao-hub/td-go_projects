package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultConfigName 默认配置文件名，落在当前构建通道的数据目录内。
const DefaultConfigName = "config.json"

// AppConfig 单个应用的图标偏好。
type AppConfig struct {
	Enabled    bool   `json:"enabled"`
	Style      string `json:"style,omitempty"`
	CustomIcon string `json:"custom_icon,omitempty"`
}

// Config 全部应用的偏好集合。
type Config struct {
	Apps map[string]AppConfig `json:"apps"`
}

// DefaultConfig 首次运行时的内置偏好快照，与仓库内历史 config.json 保持一致。
func DefaultConfig() Config {
	return Config{Apps: map[string]AppConfig{
		"vscode":           {Enabled: true, Style: "silver"},
		"deepseek_harness": {Enabled: true, Style: "8_underlit_chrome"},
		"idea":             {Enabled: false, Style: "underlit_chrome"},
		"wt":               {Enabled: true, Style: "pure_dark"},
	}}
}

// LoadConfig 读取配置；文件缺失时写入默认配置后返回。
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("读取配置文件 %q 失败: %w", path, err)
		}
		config := DefaultConfig()
		if err := SaveConfig(path, config); err != nil {
			return Config{}, err
		}
		return config, nil
	}
	config := Config{}
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("解析配置文件 %q 失败: %w", path, err)
	}
	if config.Apps == nil {
		config.Apps = map[string]AppConfig{}
	}
	return config, nil
}

// SaveConfig 原子写回配置（MkdirAll + 覆盖写，Go 默认无 BOM UTF-8）。
func SaveConfig(path string, config Config) error {
	if config.Apps == nil {
		config.Apps = map[string]AppConfig{}
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("编码配置失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	// 先写临时文件再改名，避免 GUI 与 CLI 并发读时读到半截 JSON。
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("写入配置文件 %q 失败: %w", path, err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("替换配置文件 %q 失败: %w", path, err)
	}
	return nil
}
