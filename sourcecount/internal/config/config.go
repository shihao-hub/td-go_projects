// Package config 负责 sourcecount 配置发现、解析、校验和规范化。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ConfigFileName = ".sourcestats.json"
	ConfigVersion  = 1
)

// DefaultExcludePatterns 是默认跳过的常见依赖、构建和缓存目录。
var DefaultExcludePatterns = []string{
	"**/.git", "**/.hg", "**/.svn", "**/node_modules", "**/vendor",
	"**/dist", "**/build", "**/target", "**/.idea", "**/.vscode",
	"**/.venv", "**/__pycache__", "**/coverage",
}

// Overrides 表示入口显式提供的配置覆盖。nil slice 表示未提供。
type Overrides struct {
	ConfigPath        string
	Include           []string
	Exclude           []string
	TextExtensions    []string
	BinaryExtensions  []string
	NoDefaultExcludes bool
}

// Effective 是已完成发现、默认值填充和规范化的配置。
type Effective struct {
	Include          []string
	Exclude          []string
	TextExtensions   map[string]struct{}
	BinaryExtensions map[string]struct{}
	DefaultExcludes  bool
}

type fileConfig struct {
	Version          int      `json:"version"`
	Include          []string `json:"include,omitempty"`
	Exclude          []string `json:"exclude,omitempty"`
	TextExtensions   []string `json:"text_extensions,omitempty"`
	BinaryExtensions []string `json:"binary_extensions,omitempty"`
	DefaultExcludes  *bool    `json:"default_excludes,omitempty"`
}

// Resolve 按显式路径、当前目录、单根目录、内置默认值的顺序解析配置。
func Resolve(roots []string, overrides Overrides) (Effective, string, error) {
	cfg := fileConfig{Version: ConfigVersion}
	configPath := ""
	var err error
	if overrides.ConfigPath != "" {
		configPath, err = absolutePath(overrides.ConfigPath)
		if err != nil {
			return Effective{}, "", newConfigError("config_not_found", "配置路径无效: "+err.Error())
		}
		cfg, err = readFileConfig(configPath)
		if err != nil {
			return Effective{}, configPath, err
		}
	} else if candidate, ok, discoverErr := discoverConfig(roots); discoverErr != nil {
		return Effective{}, "", discoverErr
	} else if ok {
		configPath = candidate
		cfg, err = readFileConfig(configPath)
		if err != nil {
			return Effective{}, configPath, err
		}
	}

	include := append([]string(nil), cfg.Include...)
	exclude := append([]string(nil), cfg.Exclude...)
	textExtensions := append([]string(nil), cfg.TextExtensions...)
	binaryExtensions := append([]string(nil), cfg.BinaryExtensions...)
	defaultExcludes := true
	if cfg.DefaultExcludes != nil {
		defaultExcludes = *cfg.DefaultExcludes
	}
	if overrides.Include != nil {
		include = append([]string(nil), overrides.Include...)
	}
	if overrides.Exclude != nil {
		exclude = append([]string(nil), overrides.Exclude...)
	}
	if overrides.TextExtensions != nil {
		textExtensions = append([]string(nil), overrides.TextExtensions...)
	}
	if overrides.BinaryExtensions != nil {
		binaryExtensions = append([]string(nil), overrides.BinaryExtensions...)
	}
	if overrides.NoDefaultExcludes {
		defaultExcludes = false
	}
	if defaultExcludes {
		exclude = append(append([]string(nil), DefaultExcludePatterns...), exclude...)
	}

	if err := validatePatterns(include); err != nil {
		return Effective{}, configPath, newConfigError("bad_config", err.Error())
	}
	if err := validatePatterns(exclude); err != nil {
		return Effective{}, configPath, newConfigError("bad_config", err.Error())
	}
	textSet, err := normalizeExtensions(textExtensions)
	if err != nil {
		return Effective{}, configPath, newConfigError("bad_config", err.Error())
	}
	binarySet, err := normalizeExtensions(binaryExtensions)
	if err != nil {
		return Effective{}, configPath, newConfigError("bad_config", err.Error())
	}
	for ext := range textSet {
		if _, exists := binarySet[ext]; exists {
			return Effective{}, configPath, newConfigError("bad_config", "后缀同时出现在 text_extensions 和 binary_extensions: "+ext)
		}
	}
	return Effective{
		Include:          include,
		Exclude:          exclude,
		TextExtensions:   textSet,
		BinaryExtensions: binarySet,
		DefaultExcludes:  defaultExcludes,
	}, configPath, nil
}

func discoverConfig(roots []string) (string, bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, newConfigError("config_discovery_failed", "获取当前目录失败: "+err.Error())
	}
	if candidate, ok, err := configCandidate(cwd); err != nil || ok {
		return candidate, ok, err
	}
	if len(roots) == 1 {
		info, statErr := os.Stat(roots[0])
		if statErr == nil && info.IsDir() {
			return configCandidate(roots[0])
		}
	}
	return "", false, nil
}

func configCandidate(dir string) (string, bool, error) {
	candidate := filepath.Join(dir, ConfigFileName)
	info, err := os.Stat(candidate)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, newConfigError("config_discovery_failed", "检查配置文件失败: "+err.Error())
	}
	if info.IsDir() {
		return "", false, newConfigError("bad_config", "配置路径是目录: "+candidate)
	}
	return candidate, true, nil
}

func readFileConfig(path string) (fileConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileConfig{}, newConfigError("config_not_found", "配置文件不存在: "+path)
		}
		return fileConfig{}, newConfigError("config_read_failed", "读取配置文件失败: "+err.Error())
	}
	defer file.Close()
	var cfg fileConfig
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return fileConfig{}, newConfigError("bad_config", "解析配置文件失败: "+err.Error())
	}
	if cfg.Version == 0 {
		cfg.Version = ConfigVersion
	}
	if cfg.Version != ConfigVersion {
		return fileConfig{}, newConfigError("bad_config", fmt.Sprintf("不支持的配置版本: %d", cfg.Version))
	}
	return cfg, nil
}

func absolutePath(value string) (string, error) {
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func normalizeExtensions(values []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return nil, errors.New("后缀不能是空字符串")
		}
		if !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		if strings.Contains(value, "/") || strings.Contains(value, "\\") || value == "." {
			return nil, fmt.Errorf("非法后缀: %q", value)
		}
		result[value] = struct{}{}
	}
	return result, nil
}

func validatePatterns(patterns []string) error {
	for _, pattern := range patterns {
		if _, err := NewPattern(pattern); err != nil {
			return err
		}
	}
	return nil
}

type configError struct {
	Code    string
	Message string
}

func (e *configError) Error() string            { return e.Code + ": " + e.Message }
func newConfigError(code, message string) error { return &configError{Code: code, Message: message} }

// ErrorCode 返回配置错误的稳定 code。
func ErrorCode(err error) string {
	var ce *configError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return "bad_config"
}
