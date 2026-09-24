// Package store 负责 glmquotawatch-gui 的本地持久化：config.json（配置）、
// state.json（已告警档位记录）、samples-*.jsonl（采样历史）。
// 数据目录为 %APPDATA%\language_projects\glmquotawatch-gui\，取不到 AppData 时
// 回退 ~/.language_projects/glmquotawatch-gui/；写入采用 临时文件+重命名 保证原子性。
// 单实例由 Wails SingleInstance 保证（GUI），CLI 子命令为一次性进程，
// 故不再需要归档版的 daemon.lock 跨进程锁。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config 工具配置（config.json）。业务校验（时长/阈值范围等）在 service 层，
// 本包只负责存取。
type Config struct {
	Token      string `json:"token"`
	Interval   string `json:"interval"`   // time.Duration 字符串，如 "5m"
	Thresholds []int  `json:"thresholds"` // 告警阈值（升序去重，1..99）
	Hysteresis int    `json:"hysteresis"` // 滞回百分点：低于 min-thresholds-hysteresis 才重置告警记录
	Silent     bool   `json:"silent"`     // true 时通知静音（仍发送，无声）
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{
		Token:      "",
		Interval:   "5m",
		Thresholds: []int{50, 60, 80, 90},
		Hysteresis: 5,
	}
}

// State 已告警档位记录（state.json）。Notified 按窗口稳定键记录已通知档位；
// Thresholds 是产生该记录时的阈值指纹（配置变更即整体失效、重新触发）。
type State struct {
	Version    int              `json:"version"`
	Thresholds []int            `json:"thresholds"`
	Notified   map[string][]int `json:"notified"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// Store 数据目录上的持久化操作集合，方法并发安全（单进程内）。
type Store struct {
	dir string
	mu  sync.Mutex
}

// Open 确保数据目录存在（含完整目录链）。
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir 返回数据目录路径。
func (s *Store) Dir() string { return s.dir }

// DefaultDir 返回默认数据目录：
// %APPDATA%\language_projects\glmquotawatch-gui，取不到 AppData 时回退
// ~/.language_projects/glmquotawatch-gui。
func DefaultDir() (string, error) {
	if appData := os.Getenv("AppData"); appData != "" {
		return filepath.Join(appData, "language_projects", "glmquotawatch-gui"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("定位数据目录失败: %w", err)
	}
	return filepath.Join(home, ".language_projects", "glmquotawatch-gui"), nil
}

func (s *Store) configPath() string { return filepath.Join(s.dir, "config.json") }
func (s *Store) statePath() string  { return filepath.Join(s.dir, "state.json") }

// LoadConfig 读配置；文件不存在时返回默认值（不落盘，首次保存时才产生文件）。
func (s *Store) LoadConfig() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.configPath())
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("解析 %s 失败: %w", s.configPath(), err)
	}
	return cfg, nil
}

// SaveConfig 原子写配置。
func (s *Store) SaveConfig(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.configPath(), raw)
}

// LoadState 读状态；文件不存在返回零值；JSON 损坏返回零值 + 错误
// （调用方记日志后按空状态继续即可）。
func (s *Store) LoadState() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.statePath())
	if os.IsNotExist(err) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, fmt.Errorf("解析 %s 失败: %w", s.statePath(), err)
	}
	return st, nil
}

// SaveState 原子写状态。
func (s *Store) SaveState(st State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.statePath(), raw)
}

// AppendSample 把一次采样的上游 data 原文追加到当月历史文件
// samples-YYYY-MM.jsonl（本地时区分月），返回文件路径。
// 行结构 {"ts":"<RFC3339>","data":<原文>}；失败采样不应调用本方法，
// 保证文件里全是成功样本。
func (s *Store) AppendSample(now time.Time, data json.RawMessage) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, "samples-"+now.Format("2006-01")+".jsonl")
	line := struct {
		Ts   string          `json:"ts"`
		Data json.RawMessage `json:"data"`
	}{Ts: now.Format(time.RFC3339), Data: data}
	b, err := json.Marshal(line)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return "", err
	}
	return path, nil
}

// ListSampleFiles 返回覆盖 since 时点起的采样历史文件路径（按月文件名升序）。
// 按文件名月份判断归属：当月文件的月末 >= since 即纳入（粗粒度，行级过滤由
// 调用方完成）。目录不存在返回空列表。
func (s *Store) ListSampleFiles(since time.Time) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "samples-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		month, perr := time.ParseInLocation("2006-01", strings.TrimSuffix(strings.TrimPrefix(name, "samples-"), ".jsonl"), time.Local)
		if perr != nil {
			continue // 命名异常的历史文件跳过
		}
		// 文件覆盖 [月初, 次月初)，与 since 区间有交集即纳入
		if month.AddDate(0, 1, 0).After(since) {
			out = append(out, filepath.Join(s.dir, name))
		}
	}
	sort.Strings(out)
	return out, nil
}

// ResetDir 清空并重建 target 目录内全部文件（演示模式进入时清场用）。
// 二级防线：仅接受 basename 为 "demo" 的目录（runtime 层还有全等一级校验），
// 拒绝空串、卷根等危险目标。
func (s *Store) ResetDir(target string) error {
	clean := filepath.Clean(target)
	if clean == "" || clean == "." || filepath.Base(clean) != "demo" {
		return fmt.Errorf("ResetDir 仅允许清理 demo 子目录，收到 %q", target)
	}
	if err := os.RemoveAll(clean); err != nil {
		return fmt.Errorf("清理演示目录失败: %w", err)
	}
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return fmt.Errorf("重建演示目录失败: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
