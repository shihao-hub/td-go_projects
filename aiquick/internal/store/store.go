// Package store 负责 aiquickd 的本地持久化：config.json 与 presets.json。
// 位于 %APPDATA%\aiquick\，写入采用 临时文件+重命名 保证原子性。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"aiquick/internal/api"
)

// ErrNotFound 目标预设不存在。
var ErrNotFound = errors.New("preset not found")

// DefaultDir 返回默认数据目录 %APPDATA%\aiquick。
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(base, "aiquick"), nil
}

func defaultConfig() api.Config {
	return api.Config{
		BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		APIKey:  "",
		Model:   "glm-4.7-flash",
	}
}

func seedPresets() []api.Preset {
	return []api.Preset{
		{ID: "seed-var", Name: "取变量名", System: "你是资深程序员。用户给出一段需求或中文描述，你返回 5 个合适的英文变量名候选，遵循主流命名风格（camelCase / snake_case 视场景）。每行一个，格式：变量名 - 简短中文说明。不要多余解释。"},
		{ID: "seed-func", Name: "取函数名", System: "你是资深程序员。用户给出一段需求或中文描述，你返回 5 个合适的英文函数名候选。每行一个，格式：函数名 - 简短中文说明。不要多余解释。"},
		{ID: "seed-trans", Name: "翻译成中文", System: "把用户输入的内容翻译成地道的简体中文。只输出译文，不要任何解释，也不要输出原文。"},
	}
}

// Store 内存态 + 文件持久化，并发安全。
type Store struct {
	dir string

	mu      sync.Mutex
	cfg     api.Config
	presets []api.Preset
}

// Open 打开（或初始化）数据目录；presets.json 不存在时写入种子预设。
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	s := &Store{dir: dir}

	s.cfg = defaultConfig()
	if raw, err := os.ReadFile(s.configPath()); err == nil {
		var cfg api.Config
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.configPath(), err)
		}
		s.cfg = cfg
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	s.presets = seedPresets()
	if raw, err := os.ReadFile(s.presetsPath()); err == nil {
		var ps []api.Preset
		if err := json.Unmarshal(raw, &ps); err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.presetsPath(), err)
		}
		s.presets = ps
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// 首次启动种子落盘，让用户能直接看到并修改文件
	if _, err := os.Stat(s.presetsPath()); os.IsNotExist(err) {
		if err := s.persistPresetsLocked(s.presets); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) configPath() string  { return filepath.Join(s.dir, "config.json") }
func (s *Store) presetsPath() string { return filepath.Join(s.dir, "presets.json") }

// Config 返回当前配置副本。
func (s *Store) Config() api.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// SetConfig 保存配置并落盘。
func (s *Store) SetConfig(cfg api.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.configPath(), raw)
}

// Presets 返回预设列表副本（列表顺序即展示顺序）。
func (s *Store) Presets() []api.Preset {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]api.Preset, len(s.presets))
	copy(out, s.presets)
	return out
}

// SavePreset 新增（ID 为空则生成）或更新（ID 匹配）一个预设，返回落库后的值。
func (s *Store) SavePreset(p api.Preset) (api.Preset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == "" {
		id, err := randomID()
		if err != nil {
			return p, err
		}
		p.ID = id
		s.presets = append(s.presets, p)
	} else {
		replaced := false
		for i := range s.presets {
			if s.presets[i].ID == p.ID {
				s.presets[i] = p
				replaced = true
				break
			}
		}
		if !replaced {
			s.presets = append(s.presets, p)
		}
	}
	if err := s.persistPresetsLocked(s.presets); err != nil {
		return p, err
	}
	return p, nil
}

// DeletePreset 删除指定预设；不存在返回 ErrNotFound。
func (s *Store) DeletePreset(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.presets {
		if s.presets[i].ID == id {
			s.presets = append(s.presets[:i], s.presets[i+1:]...)
			return s.persistPresetsLocked(s.presets)
		}
	}
	return ErrNotFound
}

func (s *Store) persistPresetsLocked(list []api.Preset) error {
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.presetsPath(), raw)
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func randomID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
