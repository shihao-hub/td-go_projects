package syncer

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"language_projects/go_projects/acp-title-proxy/internal/agy"
	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/proxy"
)

type Syncer struct {
	cfg       *config.Config
	zedDBPath string
	agyTitler *agy.Titler
}

func NewSyncer(cfg *config.Config) *Syncer {
	localAppData := os.Getenv("LOCALAPPDATA")
	zedDBPath := filepath.Join(localAppData, "Zed", "db", "0-stable", "db.sqlite")

	// Dummy injector for titler (we only use titler.CallLLM directly in syncer)
	inj := proxy.NewInjector(os.Stdout)
	agyTitler := agy.NewTitler(&cfg.AGY, inj)

	return &Syncer{
		cfg:       cfg,
		zedDBPath: zedDBPath,
		agyTitler: agyTitler,
	}
}

// openZedDB opens Zed's SQLite database with WAL and busy timeout configuration.
func (s *Syncer) openZedDB() (*sql.DB, error) {
	connStr := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", s.zedDBPath)
	return sql.Open("sqlite", connStr)
}

// SyncOpenCode synchronizes titles from opencode.db to Zed's sidebar_threads.
func (s *Syncer) SyncOpenCode() (int, error) {
	ocDBPath := s.cfg.OpenCode.DBPath
	if _, err := os.Stat(ocDBPath); err != nil {
		return 0, fmt.Errorf("opencode.db not found at %s", ocDBPath)
	}

	zedDB, err := s.openZedDB()
	if err != nil {
		return 0, err
	}
	defer zedDB.Close()

	ocDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro", ocDBPath))
	if err != nil {
		return 0, err
	}
	defer ocDB.Close()

	// Find threads needing titles
	query := `SELECT session_id FROM sidebar_threads 
	          WHERE agent_id = 'opencode' 
	            AND (title_override IS NULL OR title_override = '') 
	            AND session_id IS NOT NULL`
	rows, err := zedDB.Query(query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var sessionIDs []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err == nil && sid != "" {
			sessionIDs = append(sessionIDs, sid)
		}
	}
	rows.Close()

	if len(sessionIDs) == 0 {
		return 0, nil
	}

	updatedCount := 0
	tx, err := zedDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("UPDATE sidebar_threads SET title_override = ? WHERE session_id = ?")
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, sid := range sessionIDs {
		var title sql.NullString
		// First check session_v2 (OpenCode v2.2+), then fallback to legacy session
		err := ocDB.QueryRow("SELECT title FROM session_v2 WHERE id = ? LIMIT 1", sid).Scan(&title)
		if err != nil || !title.Valid {
			err = ocDB.QueryRow("SELECT title FROM session WHERE id = ? LIMIT 1", sid).Scan(&title)
		}
		if err == nil && title.Valid {
			t := strings.TrimSpace(title.String)
			if t != "" && !strings.HasPrefix(t, "New session") {
				if _, err := stmt.Exec(t, sid); err == nil {
					updatedCount++
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return updatedCount, nil
}

// SyncAGY synchronizes recent AGY threads by calling DeepSeek to generate concise titles.
// If maxAge > 0, only threads updated within maxAge are processed to prevent burning API quota.
func (s *Syncer) SyncAGY(maxAge time.Duration) (int, error) {
	if strings.TrimSpace(s.cfg.AGY.APIKey) == "" {
		return 0, nil
	}

	zedDB, err := s.openZedDB()
	if err != nil {
		return 0, err
	}
	defer zedDB.Close()

	query := `SELECT session_id, title, updated_at FROM sidebar_threads 
	          WHERE agent_id = 'antigravity-acp' 
	            AND (title_override IS NULL OR title_override = '') 
	            AND session_id IS NOT NULL`
	rows, err := zedDB.Query(query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type agyItem struct {
		sessionID string
		prompt    string
		updatedAt string
	}

	var candidates []agyItem
	cutoff := time.Now().Add(-maxAge)

	for rows.Next() {
		var sid, rawTitle, updatedAt string
		if err := rows.Scan(&sid, &rawTitle, &updatedAt); err == nil {
			if maxAge > 0 && updatedAt != "" {
				if t, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
					if t.Before(cutoff) {
						continue
					}
				}
			}
			candidates = append(candidates, agyItem{
				sessionID: sid,
				prompt:    rawTitle,
				updatedAt: updatedAt,
			})
		}
	}
	rows.Close()

	if len(candidates) == 0 {
		return 0, nil
	}

	updatedCount := 0
	for _, item := range candidates {
		prompt := strings.TrimSpace(item.prompt)
		if prompt == "" {
			continue
		}

		title, err := s.agyTitler.CallLLMDirect(prompt)
		if err != nil || title == "" {
			continue
		}

		_, err = zedDB.Exec("UPDATE sidebar_threads SET title_override = ? WHERE session_id = ?", title, item.sessionID)
		if err == nil {
			updatedCount++
		}
	}

	return updatedCount, nil
}

// Watch runs a continuous polling loop, checking for newly created or updated threads.
func (s *Syncer) Watch(interval time.Duration, agyMaxAge time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial sync
	s.runOnce(agyMaxAge)

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			s.runOnce(agyMaxAge)
		}
	}
}

func (s *Syncer) runOnce(agyMaxAge time.Duration) {
	if n, err := s.SyncOpenCode(); err == nil && n > 0 {
		fmt.Printf("[%s] [OpenCode] Synced %d titles\n", time.Now().Format("15:04:05"), n)
	}
	if n, err := s.SyncAGY(agyMaxAge); err == nil && n > 0 {
		fmt.Printf("[%s] [AGY] Synced %d titles via LLM (OpenCode Go / DeepSeek)\n", time.Now().Format("15:04:05"), n)
	}
}
