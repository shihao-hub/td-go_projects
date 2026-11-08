// Package config 负责配置默认值、校验与数据目录定位。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"rebootremind/internal/buildinfo"
	"rebootremind/internal/fsutil"
)

const (
	appName  = "rebootremind"
	fileName = "config.json"
)

// 默认阈值：开机满 3 天且内存达 85% 才提醒；满 7 天无条件温和提醒。
const (
	DefaultMinUptimeHours   = 72.0
	DefaultMinMemoryPercent = 85.0
	DefaultForceUptimeHours = 168.0
	DefaultSnoozeShortHours = 4.0
	DefaultSnoozeLongHours  = 24.0
)

// Config 是用户可调整的提醒阈值。
type Config struct {
	MinUptimeHours   float64 `json:"min_uptime_hours"`
	MinMemoryPercent float64 `json:"min_memory_percent"`
	ForceUptimeHours float64 `json:"force_uptime_hours"`
	SnoozeShortHours float64 `json:"snooze_short_hours"`
	SnoozeLongHours  float64 `json:"snooze_long_hours"`
}

// Default 返回内置默认配置。
func Default() Config {
	return Config{
		MinUptimeHours:   DefaultMinUptimeHours,
		MinMemoryPercent: DefaultMinMemoryPercent,
		ForceUptimeHours: DefaultForceUptimeHours,
		SnoozeShortHours: DefaultSnoozeShortHours,
		SnoozeLongHours:  DefaultSnoozeLongHours,
	}
}

// Validate 校验业务阈值之间的关系。
func (c Config) Validate() error {
	if c.MinUptimeHours <= 0 {
		return errors.New("min_uptime_hours 必须大于 0")
	}
	if c.MinMemoryPercent <= 0 || c.MinMemoryPercent > 100 {
		return errors.New("min_memory_percent 必须在 (0, 100] 之间")
	}
	if c.ForceUptimeHours < c.MinUptimeHours {
		return errors.New("force_uptime_hours 不得小于 min_uptime_hours")
	}
	if c.SnoozeShortHours <= 0 {
		return errors.New("snooze_short_hours 必须大于 0")
	}
	if c.SnoozeLongHours < c.SnoozeShortHours {
		return errors.New("snooze_long_hours 不得小于 snooze_short_hours")
	}
	return nil
}

// DataDir 返回当前构建形态的数据目录，并确保目录存在。
func DataDir() (string, error) {
	var base string
	if appData := os.Getenv("APPDATA"); appData != "" {
		base = filepath.Join(appData, "language_projects", appName)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("定位用户目录失败: %w", err)
		}
		base = filepath.Join(home, ".language_projects", appName)
	}
	dir := filepath.Join(base, buildinfo.Channel())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建数据目录失败: %w", err)
	}
	return dir, nil
}

// Load 读取配置；配置文件不存在时返回默认值。
func Load() (Config, error) {
	dir, err := DataDir()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, fileName))
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("读取配置失败: %w", err)
	}
	// 先填入默认值，缺省字段自动沿用默认。
	c := Default()
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("解析配置失败: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("配置非法: %w", err)
	}
	return c, nil
}

// Save 以原子方式写入配置。
func Save(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	dir, err := DataDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fsutil.WriteFileAtomic(filepath.Join(dir, fileName), data)
}
