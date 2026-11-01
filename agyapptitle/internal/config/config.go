package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ProviderConfig 定义单个模型提供方配置。
type ProviderConfig struct {
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	BaseURL         string `json:"base_url"`
	APIKey          string `json:"api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	TimeoutMs       int    `json:"timeout_ms"`
}

// Config 全局配置结构。
type Config struct {
	PollIntervalSec int              `json:"poll_interval_sec"`
	MaxAttempts     int              `json:"max_attempts"`
	Providers       []ProviderConfig `json:"providers"`
}

// GetAppDataDir 获取符合全局约束的运行时配置与数据存放目录。
func GetAppDataDir() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".language_projects", "agyapptitle")
	}
	return filepath.Join(appData, "language_projects", "agyapptitle")
}

// GetConfigPath 获取 config.json 完整绝对路径。
func GetConfigPath() string {
	return filepath.Join(GetAppDataDir(), "config.json")
}

// DefaultConfig 返回预设的开箱即用默认配置。
func DefaultConfig() *Config {
	return &Config{
		PollIntervalSec: 4,
		MaxAttempts:     3,
		Providers: []ProviderConfig{
			{
				Name:            "opencode-go",
				Enabled:         true,
				BaseURL:         "https://opencode.ai/zen/go/v1",
				APIKey:          "auto",
				Model:           "deepseek-v4.1-flash",
				ReasoningEffort: "none",
				TimeoutMs:       4000,
			},
			{
				Name:      "deepseek-official",
				Enabled:   true,
				BaseURL:   "https://api.deepseek.com/v1",
				APIKey:    "auto",
				Model:     "deepseek-chat",
				TimeoutMs: 4000,
			},
		},
	}
}

// LoadConfig 加载配置；优先读取当前应用 config.json，若不存在则尝试借用 acp-title-proxy 的配置，
// 都不存在时写入并返回默认配置。
func LoadConfig() (*Config, error) {
	cfgPath := GetConfigPath()
	if b, err := os.ReadFile(cfgPath); err == nil {
		var cfg Config
		if err := json.Unmarshal(b, &cfg); err == nil {
			resolveAutoKeys(&cfg)
			return &cfg, nil
		}
	}

	// 尝试借用已有的 acp-title-proxy 配置
	if proxyCfg := tryBorrowAcpTitleProxyConfig(); proxyCfg != nil {
		resolveAutoKeys(proxyCfg)
		// 保存一份到本工具目录
		_ = SaveConfig(proxyCfg)
		return proxyCfg, nil
	}

	// 回退默认配置
	def := DefaultConfig()
	resolveAutoKeys(def)
	_ = SaveConfig(def)
	return def, nil
}

// SaveConfig 保存配置到磁盘。
func SaveConfig(cfg *Config) error {
	dir := GetAppDataDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(GetConfigPath(), b, 0644)
}

func tryBorrowAcpTitleProxyConfig() *Config {
	candidates := []string{
		`D:\Users\language_projects\go_projects\acp-title-proxy\config.json`,
		filepath.Join(os.Getenv("APPDATA"), "language_projects", "acp-title-proxy", "config.json"),
	}

	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			var raw struct {
				AGY struct {
					Providers []ProviderConfig `json:"providers"`
				} `json:"agy"`
			}
			if err := json.Unmarshal(b, &raw); err == nil && len(raw.AGY.Providers) > 0 {
				return &Config{
					PollIntervalSec: 4,
					MaxAttempts:     3,
					Providers:       raw.AGY.Providers,
				}
			}
		}
	}
	return nil
}

func resolveAutoKeys(cfg *Config) {
	home, _ := os.UserHomeDir()
	authPath := filepath.Join(home, ".local", "share", "opencode", "auth.json")

	var ocKey string
	if b, err := os.ReadFile(authPath); err == nil {
		var auth struct {
			ApiKey string `json:"api_key"`
			Key    string `json:"key"`
		}
		_ = json.Unmarshal(b, &auth)
		if auth.ApiKey != "" {
			ocKey = auth.ApiKey
		} else if auth.Key != "" {
			ocKey = auth.Key
		}
	}

	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if strings.EqualFold(p.APIKey, "auto") || p.APIKey == "" {
			if strings.Contains(strings.ToLower(p.Name), "opencode") && ocKey != "" {
				p.APIKey = ocKey
			} else if envKey := os.Getenv("DEEPSEEK_API_KEY"); envKey != "" && strings.Contains(strings.ToLower(p.Name), "deepseek") {
				p.APIKey = envKey
			}
		}
	}
}
