// Package store 负责 termhub 的落盘资产：启动项/启动组、会话元数据、命令历史。
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

const (
	configFileName    = "entries.json"
	sessionsFileName  = "sessions.json"
	historyDirName    = "history"
	historyFileName   = "global.txt"
	maxSessionRecords = 100
)

// Entry 一个启动项：名称 + 初始目录 + 自动命令。
type Entry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	WorkDir string `json:"work_dir"`
	Command string `json:"command"`
}

// Group 启动组：按顺序拉起多个启动项。
type Group struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	EntryIDs []string `json:"entry_ids"`
}

// Config 启动项与启动组的全量配置。
type Config struct {
	Entries []Entry `json:"entries"`
	Groups  []Group `json:"groups"`
}

// SessionRecord 已退出会话的持久化元数据。
type SessionRecord struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	WorkDir    string    `json:"work_dir"`
	Cwd        string    `json:"cwd"`
	StartedAt  time.Time `json:"started_at"`
	ExitedAt   time.Time `json:"exited_at"`
	ExitCode   int       `json:"exit_code"`
	Status     string    `json:"status"`
	Commands   []string  `json:"commands"`
	LastOutput string    `json:"last_output,omitempty"`
}

// Store 聚合配置、会话记录与历史文件；所有写操作串行化。
type Store struct {
	dir string

	mu       sync.Mutex
	config   Config
	sessions []SessionRecord

	history *History
}

// New 打开（或初始化）数据目录。
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("数据目录为空")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, history: newHistory(dir)}
	if err := s.loadConfig(); err != nil {
		return nil, err
	}
	if err := s.loadSessions(); err != nil {
		return nil, err
	}
	if err := s.history.init(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir 数据目录。
func (s *Store) Dir() string { return s.dir }

// History 命令历史管理器。
func (s *Store) History() *History { return s.history }

func (s *Store) loadConfig() error {
	path := filepath.Join(s.dir, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.config = Config{Entries: []Entry{}, Groups: []Group{}}
			return nil
		}
		return err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		s.config = Config{Entries: []Entry{}, Groups: []Group{}}
		return nil
	}
	if cfg.Entries == nil {
		cfg.Entries = []Entry{}
	}
	if cfg.Groups == nil {
		cfg.Groups = []Group{}
	}
	s.config = cfg
	return nil
}

func (s *Store) loadSessions() error {
	path := filepath.Join(s.dir, sessionsFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.sessions = []SessionRecord{}
			return nil
		}
		return err
	}
	var doc struct {
		Sessions []SessionRecord `json:"sessions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		s.sessions = []SessionRecord{}
		return nil
	}
	if doc.Sessions == nil {
		doc.Sessions = []SessionRecord{}
	}
	s.sessions = doc.Sessions
	return nil
}

// Config 返回配置副本。
func (s *Store) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneConfig(s.config)
}

// ReplaceConfig 全量替换配置并落盘。
func (s *Store) ReplaceConfig(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.Entries == nil {
		cfg.Entries = []Entry{}
	}
	if cfg.Groups == nil {
		cfg.Groups = []Group{}
	}
	if err := writeJSON(filepath.Join(s.dir, configFileName), cfg); err != nil {
		return err
	}
	s.config = cfg
	return nil
}

// Entry 按 ID 查找启动项。
func (s *Store) Entry(id string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.config.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// Group 按 ID 查找启动组。
func (s *Store) Group(id string) (Group, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.config.Groups {
		if g.ID == id {
			return g, true
		}
	}
	return Group{}, false
}

// AppendSession 追加一条已退出会话记录，保留最近 maxSessionRecords 条。
func (s *Store) AppendSession(rec SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = append(s.sessions, rec)
	if len(s.sessions) > maxSessionRecords {
		s.sessions = s.sessions[len(s.sessions)-maxSessionRecords:]
	}
	doc := struct {
		Sessions []SessionRecord `json:"sessions"`
	}{Sessions: s.sessions}
	return writeJSON(filepath.Join(s.dir, sessionsFileName), doc)
}

// Sessions 返回已退出会话记录（新的在前）。
func (s *Store) Sessions() []SessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionRecord, len(s.sessions))
	for i := range s.sessions {
		out[len(s.sessions)-1-i] = cloneRecord(s.sessions[i])
	}
	return out
}

// DeleteSession 删除一条已退出会话记录。
func (s *Store) DeleteSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]SessionRecord, 0, len(s.sessions))
	found := false
	for _, rec := range s.sessions {
		if rec.ID == id {
			found = true
			continue
		}
		kept = append(kept, rec)
	}
	if !found {
		return fmt.Errorf("会话记录不存在: %s", id)
	}
	s.sessions = kept
	doc := struct {
		Sessions []SessionRecord `json:"sessions"`
	}{Sessions: s.sessions}
	return writeJSON(filepath.Join(s.dir, sessionsFileName), doc)
}

// Session 按 ID 查找已退出会话记录。
func (s *Store) Session(id string) (SessionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.sessions {
		if rec.ID == id {
			return cloneRecord(rec), true
		}
	}
	return SessionRecord{}, false
}

func cloneConfig(cfg Config) Config {
	out := Config{Entries: make([]Entry, len(cfg.Entries)), Groups: make([]Group, len(cfg.Groups))}
	copy(out.Entries, cfg.Entries)
	for i, g := range cfg.Groups {
		g.EntryIDs = append([]string(nil), g.EntryIDs...)
		out.Groups[i] = g
	}
	return out
}

func cloneRecord(rec SessionRecord) SessionRecord {
	rec.Commands = append([]string(nil), rec.Commands...)
	return rec
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// NewID 生成带前缀的唯一 ID。
func NewID(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
}

// History 命令历史：每会话独立文件 + 全局合并文件。
type History struct {
	root       string
	sessions   string
	globalPath string
	mu         sync.Mutex
}

func newHistory(dir string) *History {
	root := filepath.Join(dir, historyDirName)
	return &History{
		root:       root,
		sessions:   filepath.Join(root, "sessions"),
		globalPath: filepath.Join(root, historyFileName),
	}
}

func (h *History) init() error {
	if err := os.MkdirAll(h.sessions, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(h.globalPath); os.IsNotExist(err) {
		lines := importSystemHistory()
		return writeLines(h.globalPath, lines)
	}
	return nil
}

// SessionPath 会话命令文件路径。
func (h *History) SessionPath(id string) string {
	return filepath.Join(h.sessions, id+".txt")
}

// BootstrapPath 会话启动脚本路径。
func (h *History) BootstrapPath(id string) string {
	return filepath.Join(h.sessions, id+".ps1")
}

// SessionCwdPath 会话当前工作目录文件路径。
func (h *History) SessionCwdPath(id string) string {
	return filepath.Join(h.sessions, id+".cwd")
}

// Prepare 创建空的会话历史文件（无 BOM）。
func (h *History) Prepare(id string) error {
	return writeLines(h.SessionPath(id), nil)
}

// ReadSession 读取会话已记录的命令。
func (h *History) ReadSession(id string) []string {
	lines, _ := readLines(h.SessionPath(id))
	return lines
}

// AppendGlobal 把新命令追加到全局历史。
func (h *History) AppendGlobal(commands []string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	existing, _ := readLines(h.globalPath)
	existing = append(existing, commands...)
	return writeLines(h.globalPath, existing)
}

// Global 读取全局命令历史。
func (h *History) Global() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	lines, _ := readLines(h.globalPath)
	return lines
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(data), "\uFEFF")
	raw := strings.Split(text, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSuffix(line, "\r")
		if line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}

func writeLines(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	text := ""
	if len(lines) > 0 {
		text = strings.Join(lines, "\n") + "\n"
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// importSystemHistory 首次启动时导入系统 PSReadLine 历史，作为全局历史基线。
func importSystemHistory() []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 512)
	for _, path := range systemHistoryPaths() {
		lines, err := readLines(path)
		if err != nil {
			continue
		}
		for _, line := range lines {
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
			out = append(out, line)
		}
	}
	return out
}

func systemHistoryPaths() []string {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		return nil
	}
	return []string{
		filepath.Join(configDir, "Microsoft", "Windows", "PowerShell", "PSReadLine", "ConsoleHost_history.txt"),
		filepath.Join(configDir, "Microsoft", "PowerShell", "PSReadLine", "ConsoleHost_history.txt"),
	}
}

// SortSessions 按开始时间倒序（供 UI 使用）。
func SortSessions(records []SessionRecord) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].StartedAt.After(records[j].StartedAt)
	})
}
