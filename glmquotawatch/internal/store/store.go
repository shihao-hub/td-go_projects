// Package store 负责 glmquotawatch 的本地持久化：config.json（配置）、
// state.json（告警武装状态）、samples-*.jsonl（采样历史）与 daemon.lock（单实例锁）。
// 数据目录为 %APPDATA%\language_projects\glmquotawatch\，取不到 AppData 时
// 回退 ~/.language_projects/glmquotawatch/；写入采用 临时文件+重命名 保证原子性。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	Hysteresis int    `json:"hysteresis"` // 滞回百分点：低于 min-thresholds-hysteresis 才重新武装
	Silent     bool   `json:"silent"`     // true 时通知静音
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

// State 告警武装状态（state.json）。Notified 按窗口稳定键记录已通知档位；
// Thresholds 是产生该状态时的阈值指纹（配置变更即整体失效、重新武装）。
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
// %APPDATA%\language_projects\glmquotawatch，取不到 AppData 时回退
// ~/.language_projects/glmquotawatch。
func DefaultDir() (string, error) {
	if appData := os.Getenv("AppData"); appData != "" {
		return filepath.Join(appData, "language_projects", "glmquotawatch"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("定位数据目录失败: %w", err)
	}
	return filepath.Join(home, ".language_projects", "glmquotawatch"), nil
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

// LockError 单实例锁被占用。
type LockError struct{ PID int }

func (e *LockError) Error() string {
	return fmt.Sprintf("另一个监控实例正在运行（PID %d）；若确认没有，可删除数据目录下的 daemon.lock", e.PID)
}

// Lock 获取单实例锁：向 daemon.lock 写入当前进程 PID。
// 已有锁且其中 PID 仍存活时返回 *LockError；否则覆盖。返回的释放函数删除锁文件。
func (s *Store) Lock() (release func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, "daemon.lock")
	if raw, rerr := os.ReadFile(path); rerr == nil {
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(raw))); perr == nil && pidAlive(pid) {
			return nil, &LockError{PID: pid}
		}
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return nil, fmt.Errorf("写入 daemon.lock 失败: %w", err)
	}
	return func() { _ = os.Remove(path) }, nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
