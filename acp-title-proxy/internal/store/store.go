package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Store 线程安全地持久化记录已命名的 ACP 会话。
type Store struct {
	filePath string
	mu       sync.RWMutex
	titled   map[string]string // sessionID -> 标题
}

// GetDataFilePath 返回符合仓库数据落盘规范的标准存储路径：
// %APPDATA%\language_projects\acp-title-proxy\titled_sessions.json
func GetDataFilePath() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, ".language_projects", "acp-title-proxy", "titled_sessions.json")
		}
		return filepath.Join(".", "titled_sessions.json")
	}
	return filepath.Join(appData, "language_projects", "acp-title-proxy", "titled_sessions.json")
}

// NewStore 从磁盘加载已命名会话，或创建空存储。
func NewStore(customPath string) (*Store, error) {
	fp := customPath
	if fp == "" {
		fp = GetDataFilePath()
	}

	s := &Store{
		filePath: fp,
		titled:   make(map[string]string),
	}

	if data, err := os.ReadFile(fp); err == nil {
		var loaded map[string]string
		if err := json.Unmarshal(data, &loaded); err == nil && loaded != nil {
			s.titled = loaded
		}
	}

	return s, nil
}

// IsTitled 判断该会话是否已被命名。
func (s *Store) IsTitled(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.titled[sessionID]
	return exists
}

// GetTitle 返回该会话已记录的标题（若存在）。
func (s *Store) GetTitle(sessionID string) (string, bool) {
	if sessionID == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	title, exists := s.titled[sessionID]
	return title, exists
}

// MarkTitled 将会话登记为已命名并持久化到磁盘。
func (s *Store) MarkTitled(sessionID, title string) error {
	if sessionID == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.titled[sessionID] = title
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.titled, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpFile, s.filePath)
}
