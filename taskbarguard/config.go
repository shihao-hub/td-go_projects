package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const defaultConfigFile = "config.json"

type AppConfig struct {
	Enabled    bool   `json:"enabled"`
	Style      string `json:"style,omitempty"`
	CustomIcon string `json:"custom_icon,omitempty"`
}

type Config struct {
	Apps map[string]AppConfig `json:"apps"`
}

func defaultConfig() Config {
	return Config{Apps: map[string]AppConfig{
		"vscode":           {Enabled: true, Style: "blue"},
		"deepseek_harness": {Enabled: true, Style: "8_underlit_chrome"},
	}}
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("读取配置文件 %q 失败: %w", path, err)
		}
		config := defaultConfig()
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
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("写入配置文件 %q 失败: %w", path, err)
	}
	return nil
}
