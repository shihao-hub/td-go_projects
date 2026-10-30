package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Store provides thread-safe persistent tracking of titled ACP sessions.
type Store struct {
	filePath string
	mu       sync.RWMutex
	titled   map[string]string // sessionID -> title
}

// GetDataFilePath returns the standard storage path conforming to repository invariants:
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

// NewStore loads existing titled sessions from disk or creates a new empty store.
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

// IsTitled checks whether the sessionID has already been titled.
func (s *Store) IsTitled(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.titled[sessionID]
	return exists
}

// GetTitle returns the recorded title for the sessionID, if present.
func (s *Store) GetTitle(sessionID string) (string, bool) {
	if sessionID == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	title, exists := s.titled[sessionID]
	return title, exists
}

// MarkTitled records a session as titled and persists it to disk.
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
