package syncer

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"language_projects/go_projects/acp-title-proxy/internal/agy"
	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/proxy"
)

// agyRetryBackoff 是 AGY 标题生成失败后的重试冷却时间，
// 避免 watch 模式每轮轮询都对失败会话重复调用模型。
const agyRetryBackoff = 10 * time.Minute

type Syncer struct {
	cfg       *config.Config
	zedDBPath string
	agyTitler *agy.Titler

	mu     sync.Mutex
	failed map[string]time.Time // sessionID -> 冷却截止时间
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
		failed:    make(map[string]time.Time),
	}
}

// hasUsableProvider 判断是否存在至少一个启用且带密钥的模型提供方。
func (s *Syncer) hasUsableProvider() bool {
	for _, p := range s.cfg.AGY.Providers {
		if p.Enabled && strings.TrimSpace(p.APIKey) != "" {
			return true
		}
	}
	return false
}

// inBackoff 判断会话是否仍处于失败冷却期。
func (s *Syncer) inBackoff(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.failed[sessionID]
	return ok && time.Now().Before(until)
}

func (s *Syncer) markFailed(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed[sessionID] = time.Now().Add(agyRetryBackoff)
}

func (s *Syncer) clearFailed(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failed, sessionID)
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

	// 找出需要生成标题的线程（含自建 agent 名，如 "Opencode (auto-title)"）
	query := `SELECT session_id FROM sidebar_threads 
	          WHERE agent_id LIKE 'opencode%' 
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
	if !s.hasUsableProvider() {
		log.Printf("[AGY] 跳过：未配置可用的模型提供方（providers 为空，或全部禁用/缺密钥）")
		return 0, nil
	}

	zedDB, err := s.openZedDB()
	if err != nil {
		return 0, err
	}
	defer zedDB.Close()

	// 同时覆盖官方 antigravity-acp 与自建 "Google Antigravity (auto-title)" 等变体
	query := `SELECT session_id, title, updated_at FROM sidebar_threads 
	          WHERE agent_id LIKE '%antigravity%' 
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

		// 失败冷却期内跳过，避免 watch 每轮重复烧配额
		if s.inBackoff(item.sessionID) {
			continue
		}

		title, err := s.agyTitler.CallLLMDirect(prompt)
		if err != nil || title == "" {
			s.markFailed(item.sessionID)
			log.Printf("[AGY] 会话 %s 标题生成失败（%s 后重试）: %v", item.sessionID, agyRetryBackoff, err)
			continue
		}

		if _, err := zedDB.Exec("UPDATE sidebar_threads SET title_override = ? WHERE session_id = ?", title, item.sessionID); err != nil {
			s.markFailed(item.sessionID)
			log.Printf("[AGY] 会话 %s 标题写入失败（%s 后重试）: %v", item.sessionID, agyRetryBackoff, err)
			continue
		}

		s.clearFailed(item.sessionID)
		updatedCount++
		log.Printf("[AGY] 会话 %s 标题已同步: %s", item.sessionID, title)
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
	if n, err := s.SyncOpenCode(); err != nil {
		log.Printf("[OpenCode] 同步失败: %v", err)
	} else if n > 0 {
		log.Printf("[OpenCode] 已同步 %d 个标题", n)
	}

	if n, err := s.SyncAGY(agyMaxAge); err != nil {
		log.Printf("[AGY] 同步失败: %v", err)
	} else if n > 0 {
		log.Printf("[AGY] 已同步 %d 个标题", n)
	}
}
