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
	terminalsFileName = "terminals.json"
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

// SessionRecord 已退出会话的持久化元数据（内部诊断/命令数据源，不进 UI）。
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

// TerminalSlot 一条终端记忆：用户开过的终端与其自动重开偏好。
// 列表随用户增删；重启后原样恢复，不再引入「已退出留存」概念。
type TerminalSlot struct {
	ID      string `json:"id"`
	WorkDir string `json:"work_dir"`
	// Source 首次创建时的来源：manual / entry:<id> / group:<id> / restore:<id>。
	Source string `json:"source,omitempty"`
	// AutoStart 勾选后，termhub 下次启动自动以相同目录拉起一个纯净 shell。
	AutoStart bool      `json:"auto_start"`
	CreatedAt time.Time `json:"created_at"`
	// LastSessionID 最近一次 shell 运行（供诊断/命令清单读取上下文）。
	LastSessionID string `json:"last_session_id,omitempty"`
}

// Store 聚合配置、终端记忆、会话记录与历史文件；所有写操作串行化。
type Store struct {
	dir string

	mu        sync.Mutex
	config    Config
	sessions  []SessionRecord
	terminals []TerminalSlot

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
	// 清理上次写入中断残留的临时文件，避免与数据文件同名冲突。
	for _, name := range []string{configFileName, sessionsFileName, terminalsFileName} {
		_ = os.Remove(filepath.Join(dir, name+".tmp"))
	}
	s := &Store{dir: dir, history: newHistory(dir)}
	if err := s.loadConfig(); err != nil {
		return nil, err
	}
	if err := s.loadSessions(); err != nil {
		return nil, err
	}
	if err := s.loadTerminals(); err != nil {
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

func (s *Store) loadTerminals() error {
	path := filepath.Join(s.dir, terminalsFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.terminals = []TerminalSlot{}
			return nil
		}
		return err
	}
	var doc struct {
		Terminals []TerminalSlot `json:"terminals"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		s.terminals = []TerminalSlot{}
		return nil
	}
	if doc.Terminals == nil {
		doc.Terminals = []TerminalSlot{}
	}
	s.terminals = doc.Terminals
	return nil
}

func (s *Store) saveTerminalsLocked() error {
	doc := struct {
		Terminals []TerminalSlot `json:"terminals"`
	}{Terminals: s.terminals}
	return writeJSON(filepath.Join(s.dir, terminalsFileName), doc)
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

// Terminals 返回全部终端记忆（按创建时间升序，保持用户开终端的先后顺序）。
func (s *Store) Terminals() []TerminalSlot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TerminalSlot, len(s.terminals))
	copy(out, s.terminals)
	return out
}

// TerminalByID 按 ID 查找终端记忆。
func (s *Store) TerminalByID(id string) (TerminalSlot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, slot := range s.terminals {
		if slot.ID == id {
			return cloneTerminal(slot), true
		}
	}
	return TerminalSlot{}, false
}

// AppendTerminal 新建一条终端记忆（生成稳定 ID）并落盘，返回新条目。
func (s *Store) AppendTerminal(workDir, source string) (TerminalSlot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot := TerminalSlot{
		ID:        NewID("t"),
		WorkDir:   workDir,
		Source:    source,
		CreatedAt: time.Now(),
	}
	s.terminals = append(s.terminals, slot)
	if err := s.saveTerminalsLocked(); err != nil {
		return TerminalSlot{}, err
	}
	return cloneTerminal(slot), nil
}

// DeleteTerminal 删除一条终端记忆；不存在时报错。
func (s *Store) DeleteTerminal(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]TerminalSlot, 0, len(s.terminals))
	found := false
	for _, slot := range s.terminals {
		if slot.ID == id {
			found = true
			continue
		}
		kept = append(kept, slot)
	}
	if !found {
		return fmt.Errorf("终端记录不存在: %s", id)
	}
	s.terminals = kept
	return s.saveTerminalsLocked()
}

// UpdateTerminalAutoStart 更新自动重开勾选；不存在时报错。
func (s *Store) UpdateTerminalAutoStart(id string, on bool) error {
	return s.updateTerminal(id, func(slot *TerminalSlot) { slot.AutoStart = on })
}

// UpdateTerminalLastSession 绑定最近一次 shell 运行；不存在时报错。
func (s *Store) UpdateTerminalLastSession(id, sessionID string) error {
	return s.updateTerminal(id, func(slot *TerminalSlot) { slot.LastSessionID = sessionID })
}

func (s *Store) updateTerminal(id string, mutate func(*TerminalSlot)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.terminals {
		if s.terminals[i].ID == id {
			mutate(&s.terminals[i])
			return s.saveTerminalsLocked()
		}
	}
	return fmt.Errorf("终端记录不存在: %s", id)
}

func cloneTerminal(slot TerminalSlot) TerminalSlot { return slot }

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
	if err := renameWithRetry(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// renameWithRetry 重命名并短退避重试；
// Windows 上目标被瞬时句柄占用（杀软扫描/外部读取工具等）时 rename 返回 Access denied，
// 通常几百毫秒内解锁，重试可吸收这类瞬时锁。全失败返回最后一次错误。
func renameWithRetry(src, dst string) error {
	backoff := []time.Duration{0, 20 * time.Millisecond, 80 * time.Millisecond, 200 * time.Millisecond}
	var err error
	for _, delay := range backoff {
		if delay > 0 {
			time.Sleep(delay)
		}
		err = os.Rename(src, dst)
		if err == nil {
			return nil
		}
	}
	return err
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

// GlobalPath 全局命令历史文件路径。
func (h *History) GlobalPath() string { return h.globalPath }

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

// ScreenPath 会话最近一屏（raw 终端字节）文件路径。
func (h *History) ScreenPath(id string) string {
	return filepath.Join(h.sessions, id+".screen")
}

// WriteSessionScreen 保全会话最近一屏（raw bytes），供终端重开时回放。
func (h *History) WriteSessionScreen(id string, data []byte) error {
	if err := os.MkdirAll(h.sessions, 0o755); err != nil {
		return err
	}
	return os.WriteFile(h.ScreenPath(id), data, 0o644)
}

// ReadSessionScreen 读取会话最近一屏；缺失返回空。
func (h *History) ReadSessionScreen(id string) []byte {
	data, err := os.ReadFile(h.ScreenPath(id))
	if err != nil {
		return nil
	}
	return data
}

// Cleanup 清理一个会话遗留的命令/脚本/目录/屏幕文件（用户删除终端记忆时调用）。
// 文件不存在时静默跳过。
func (h *History) Cleanup(id string) {
	for _, suffix := range []string{".txt", ".ps1", ".cwd", ".screen"} {
		_ = os.Remove(filepath.Join(h.sessions, id+suffix))
	}
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
