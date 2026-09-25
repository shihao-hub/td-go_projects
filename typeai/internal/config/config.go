package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	envBaseURL = "TYPEAI_BASE_URL"
	envAPIKey  = "TYPEAI_API_KEY"
	envModel   = "TYPEAI_MODEL"
)

// ValidationError 表示用户提供的配置不合法。
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Config 是写入 config.json 的持久化形态。
type Config struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

// DefaultDir 返回 %APPDATA%\language_projects\typeai；无 APPDATA 时回退用户目录。
func DefaultDir() (string, error) {
	if appData := firstNonEmpty(os.Getenv("APPDATA"), os.Getenv("AppData")); appData != "" {
		return filepath.Join(appData, "language_projects", "typeai"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("定位数据目录失败: %w", err)
	}
	return filepath.Join(home, ".language_projects", "typeai"), nil
}

func defaults() Config {
	return Config{
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4o-mini",
	}
}

// Load 读取磁盘配置；配置文件不存在时返回内置默认值，不创建目录。
func Load(dir string) (Config, error) {
	cfg, err := LoadDisk(dir)
	if err != nil {
		return cfg, err
	}
	return applyEnvironment(cfg), nil
}

// LoadDisk 只读取 config.json；不存在时返回内置默认值，不应用环境变量。
func LoadDisk(dir string) (Config, error) {
	cfg := defaults()
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("读取配置失败: %w", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置失败: %w", err)
	}
	return cfg, nil
}

// Save 校验并原子写入配置文件，写入前创建完整目录链。
func Save(dir string, cfg Config) error {
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Model = strings.TrimSpace(cfg.Model)
	if err := Validate(cfg); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	return writeFileAtomic(filepath.Join(dir, "config.json"), raw)
}

// Validate 校验配置的业务边界，不修改配置值。
func Validate(cfg Config) error {
	if cfg.BaseURL == "" {
		return &ValidationError{Message: "base_url 不能为空"}
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return &ValidationError{Message: "base_url 必须是合法 http(s) 地址"}
	}
	if cfg.Model == "" {
		return &ValidationError{Message: "model 不能为空"}
	}
	return nil
}

// Path 返回配置文件路径，供 CLI 提示使用。
func Path(dir string) string {
	return filepath.Join(dir, "config.json")
}

func applyEnvironment(cfg Config) Config {
	if v := strings.TrimSpace(os.Getenv(envBaseURL)); v != "" {
		cfg.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv(envAPIKey)); v != "" {
		cfg.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv(envModel)); v != "" {
		cfg.Model = v
	}
	return cfg
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("同步临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	return nil
}
