package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	OpenCode OpenCodeConfig `json:"opencode"`
	AGY      AGYConfig      `json:"agy"`
	Skills   SkillsConfig   `json:"skills"`
}

type SkillsConfig struct {
	Enabled          bool     `json:"enabled"`
	Prefix           string   `json:"prefix"`
	Dirs             []string `json:"dirs"`
	AutoInjectPrompt bool     `json:"auto_inject_prompt"`
}

type OpenCodeConfig struct {
	Executable     string `json:"executable"`
	DBPath         string `json:"db_path"`
	PollIntervalMs int    `json:"poll_interval_ms"`
	MaxPolls       int    `json:"max_polls"`
}

type ProviderConfig struct {
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	BaseURL         string `json:"base_url"`
	APIKey          string `json:"api_key"` // 可设为 "auto"，从 ~/.local/share/opencode/auth.json 读取
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"` // 取值如 "none"、"low" 等
	TimeoutMs       int    `json:"timeout_ms"`
}

type AGYConfig struct {
	Executable string           `json:"executable"`
	Providers  []ProviderConfig `json:"providers"`

	// 旧的单提供方配置字段（保留向后兼容）
	BaseURL   string `json:"base_url,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
	Model     string `json:"model,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

func DefaultConfig() *Config {
	return &Config{
		OpenCode: OpenCodeConfig{
			DBPath:         filepath.Join(os.Getenv("USERPROFILE"), ".local", "share", "opencode", "opencode.db"),
			PollIntervalMs: 1500,
			MaxPolls:       8,
		},
		AGY: AGYConfig{
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
					Name:            "deepseek-official",
					Enabled:         true,
					BaseURL:         "https://api.deepseek.com/v1",
					APIKey:          "",
					Model:           "deepseek-flash",
					ReasoningEffort: "none",
					TimeoutMs:       4000,
				},
			},
		},
		Skills: SkillsConfig{
			Enabled:          true,
			Prefix:           "skill:",
			Dirs:             DefaultSkillDirs(),
			AutoInjectPrompt: true,
		},
	}
}

// DefaultSkillDirs 返回技能目录的标准搜索路径。
func DefaultSkillDirs() []string {
	userProfile := os.Getenv("USERPROFILE")
	if userProfile == "" {
		userProfile = os.Getenv("HOME")
	}
	return []string{
		filepath.Join(userProfile, ".agents", "skills"),
		filepath.Join(userProfile, ".gemini", "antigravity", "builtin", "skills"),
	}
}

// ExpandEnv 将 %VAR% 或 $VAR 替换为环境变量值。
func ExpandEnv(path string) string {
	if strings.Contains(path, "%") {
		parts := strings.Split(path, "%")
		for i := 1; i < len(parts); i += 2 {
			val := os.Getenv(parts[i])
			path = strings.ReplaceAll(path, "%"+parts[i]+"%", val)
		}
	}
	return os.ExpandEnv(path)
}

// ResolveAuthKey 按需从 ~/.local/share/opencode/auth.json 读取指定提供方的密钥。
func ResolveAuthKey(providerName string) string {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home = os.Getenv("HOME")
	}
	authPath := filepath.Join(home, ".local", "share", "opencode", "auth.json")
	data, err := os.ReadFile(authPath)
	if err != nil {
		return ""
	}

	var authMap map[string]struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	if err := json.Unmarshal(data, &authMap); err != nil {
		return ""
	}

	if item, ok := authMap[providerName]; ok {
		return item.Key
	}
	return ""
}

// LoadConfig 在若干已知位置查找 config.json。
func LoadConfig() (*Config, string, error) {
	cfg := DefaultConfig()

	candidateDirs := []string{}
	if exePath, err := os.Executable(); err == nil {
		candidateDirs = append(candidateDirs, filepath.Dir(exePath))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidateDirs = append(candidateDirs, cwd)
		candidateDirs = append(candidateDirs, filepath.Dir(cwd))
		candidateDirs = append(candidateDirs, filepath.Dir(filepath.Dir(cwd)))
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		candidateDirs = append(candidateDirs, filepath.Join(appData, "acp-title-proxy"))
	}

	foundPath := ""
	for _, dir := range candidateDirs {
		cfgPath := filepath.Join(dir, "config.json")
		if data, err := os.ReadFile(cfgPath); err == nil {
			if err := json.Unmarshal(data, cfg); err == nil {
				foundPath = cfgPath
				break
			}
		}
	}

	cfg.OpenCode.DBPath = ExpandEnv(cfg.OpenCode.DBPath)
	if cfg.OpenCode.PollIntervalMs <= 0 {
		cfg.OpenCode.PollIntervalMs = 1500
	}
	if cfg.OpenCode.MaxPolls <= 0 {
		cfg.OpenCode.MaxPolls = 8
	}

	// 若存在旧的单提供方配置且 providers 列表为空，则迁移为多提供方结构
	if len(cfg.AGY.Providers) == 0 && cfg.AGY.BaseURL != "" {
		cfg.AGY.Providers = append(cfg.AGY.Providers, ProviderConfig{
			Name:      "legacy",
			Enabled:   true,
			BaseURL:   cfg.AGY.BaseURL,
			APIKey:    cfg.AGY.APIKey,
			Model:     cfg.AGY.Model,
			TimeoutMs: cfg.AGY.TimeoutMs,
		})
	}

	// 解析 "auto" 密钥并补全超时默认值
	for i := range cfg.AGY.Providers {
		p := &cfg.AGY.Providers[i]
		if p.TimeoutMs <= 0 {
			p.TimeoutMs = 4000
		}
		if p.APIKey == "auto" {
			if key := ResolveAuthKey(p.Name); key != "" {
				p.APIKey = key
			}
		}
	}

	if len(cfg.Skills.Dirs) == 0 {
		cfg.Skills.Dirs = DefaultSkillDirs()
	}

	return cfg, foundPath, nil
}
