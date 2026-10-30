package opencode

import (
	"database/sql"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/proxy"
)

type Titler struct {
	cfg      *config.OpenCodeConfig
	injector *proxy.Injector

	mu             sync.Mutex
	titledSessions map[string]bool
	activePolls    map[string]bool
}

func NewTitler(cfg *config.OpenCodeConfig, inj *proxy.Injector) *Titler {
	return &Titler{
		cfg:            cfg,
		injector:       inj,
		titledSessions: make(map[string]bool),
		activePolls:    make(map[string]bool),
	}
}

func (t *Titler) OnSessionNew(sessionID string) {
	// Ready to accept prompts for this session
}

func (t *Titler) OnPrompt(sessionID string, prompt string) {
	t.mu.Lock()
	if t.titledSessions[sessionID] || t.activePolls[sessionID] {
		t.mu.Unlock()
		return
	}
	t.activePolls[sessionID] = true
	t.mu.Unlock()

	go t.pollSessionTitle(sessionID)
}

func (t *Titler) pollSessionTitle(sessionID string) {
	interval := time.Duration(t.cfg.PollIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 1500 * time.Millisecond
	}
	maxPolls := t.cfg.MaxPolls
	if maxPolls <= 0 {
		maxPolls = 8
	}

	for i := 0; i < maxPolls; i++ {
		time.Sleep(interval)

		t.mu.Lock()
		if t.titledSessions[sessionID] {
			t.mu.Unlock()
			return
		}
		t.mu.Unlock()

		title, found := t.queryDB(sessionID)
		if found && title != "" && !strings.HasPrefix(title, "New session") {
			// Found valid title! Push to Zed.
			if err := t.injector.InjectTitle(sessionID, title); err == nil {
				t.mu.Lock()
				t.titledSessions[sessionID] = true
				delete(t.activePolls, sessionID)
				t.mu.Unlock()
				return
			}
		}
	}

	t.mu.Lock()
	delete(t.activePolls, sessionID)
	t.mu.Unlock()
}

func (t *Titler) queryDB(sessionID string) (string, bool) {
	db, err := sql.Open("sqlite", t.cfg.DBPath)
	if err != nil {
		return "", false
	}
	defer db.Close()

	var title sql.NullString
	// OpenCode v2.2+ uses session_v2, legacy uses session
	query := "SELECT title FROM session_v2 WHERE id = ? LIMIT 1"
	err = db.QueryRow(query, sessionID).Scan(&title)
	if err != nil || !title.Valid {
		query = "SELECT title FROM session WHERE id = ? LIMIT 1"
		err = db.QueryRow(query, sessionID).Scan(&title)
	}
	if err != nil || !title.Valid {
		return "", false
	}

	trimmed := strings.TrimSpace(title.String)
	if trimmed == "" {
		return "", false
	}

	return trimmed, true
}
