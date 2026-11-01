package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"language_projects/go_projects/agyapptitle/internal/config"
)

// State 持久化记录守护进程的处理进度与失败熔断计数。
type State struct {
	mu             sync.Mutex
	path           string
	TitledSessions map[string]string `json:"titled_sessions"` // session_id -> title
	Attempts       map[string]int    `json:"attempts"`        // session_id -> fail_count
	LastScanTime   string            `json:"last_scan_time"`
}

// LoadState 加载或初始化持久化状态。
func LoadState() (*State, error) {
	statePath := filepath.Join(config.GetAppDataDir(), "state.json")
	st := &State{
		path:           statePath,
		TitledSessions: make(map[string]string),
		Attempts:       make(map[string]int),
	}

	if b, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(b, st)
		if st.TitledSessions == nil {
			st.TitledSessions = make(map[string]string)
		}
		if st.Attempts == nil {
			st.Attempts = make(map[string]int)
		}
	}
	return st, nil
}

// IsTitled 判断会话是否已被本工具成功汉化过。
func (s *State) IsTitled(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.TitledSessions[sessionID]
	return ok
}

// CanAttempt 检查会话是否未超过最大重试失败次数（熔断机制）。
func (s *State) CanAttempt(sessionID string, maxAttempts int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Attempts[sessionID] < maxAttempts
}

// RecordSuccess 记录会话汉化成功。
func (s *State) RecordSuccess(sessionID, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TitledSessions[sessionID] = title
	delete(s.Attempts, sessionID)
	s.LastScanTime = time.Now().Format(time.RFC3339)
	_ = s.saveLocked()
}

// RecordFailure 递增失败重试次数。
func (s *State) RecordFailure(sessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Attempts[sessionID]++
	count := s.Attempts[sessionID]
	s.LastScanTime = time.Now().Format(time.RFC3339)
	_ = s.saveLocked()
	return count
}

func (s *State) saveLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}
