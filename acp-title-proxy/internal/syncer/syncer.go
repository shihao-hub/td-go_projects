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

	// 供 titler 使用的占位 Injector（syncer 中只直接调用 titler.CallLLM）
	inj := proxy.NewInjector(os.Stdout)
	agyTitler := agy.NewTitler(&cfg.AGY, inj, nil)

	return &Syncer{
		cfg:       cfg,
		zedDBPath: zedDBPath,
		agyTitler: agyTitler,
	}
}

// openZedDB 以 WAL 与 busy timeout 配置打开 Zed 的 SQLite 数据库。
func (s *Syncer) openZedDB() (*sql.DB, error) {
	connStr := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", s.zedDBPath)
	return sql.Open("sqlite", connStr)
}

// SyncOpenCode 将 opencode.db 中的标题同步到 Zed 的 sidebar_threads。
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

	// 找出需要生成标题的线程
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
		// 先查 session_v2（OpenCode v2.2+），再回退到旧的 session 表
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

// SyncAGY 调用 DeepSeek 为最近的 AGY 线程生成简洁标题并同步。
// 若 maxAge > 0，仅处理该时间窗内更新的线程，避免浪费 API 配额。
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

// Watch 持续轮询，检查新建或更新的线程。
func (s *Syncer) Watch(interval time.Duration, agyMaxAge time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 启动时先做一次同步
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
