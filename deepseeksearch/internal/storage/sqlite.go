package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// sqliteDSNParams 是 SQLite 并发连接的基础守卫：
// WAL 模式允许读写互不阻塞；busy_timeout 5s 避免并发写报 database is locked。
const sqliteDSNParams = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"

// dbTimeLayout 与 SQLite CURRENT_TIMESTAMP 的墙钟格式一致。
// 关键约束：CURRENT_TIMESTAMP 恒为 UTC，本地侧所有比较与解析必须显式 UTC——
// 若混入本地时区，UTC+8 机器上的缓存时效窗口会漂移 ±8 小时。
const dbTimeLayout = "2006-01-02 15:04:05"

// dbCutoff 计算往前回溯 maxAge 的 UTC 墙钟串，供 SQL 字符串比较（>= cutoff）使用。
func dbCutoff(maxAge time.Duration) string {
	return time.Now().UTC().Add(-maxAge).Format(dbTimeLayout)
}

// parseDBTime 解析 SQLite 写入的 UTC 墙钟时间；
// 历史库兼容少量以 RFC3339 落盘的字符串。返回的时间恒为 UTC 时刻，
// 对外展示前由调用方按需 .Local() 转本地时区。
func parseDBTime(s string) (time.Time, error) {
	if t, err := time.ParseInLocation(dbTimeLayout, s, time.UTC); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

type AppConfig struct {
	APIKey       string `json:"apiKey"`
	BaseURL      string `json:"baseUrl"`
	DefaultCount int    `json:"defaultCount"`
	NeedContent  bool   `json:"needContent"`
	UpdatedAt    string `json:"updatedAt"`
}

type SearchRecord struct {
	ID          int64     `json:"id"`
	Query       string    `json:"query"`
	Count       int       `json:"count"`
	NeedContent bool      `json:"needContent"`
	ResultCount int       `json:"resultCount"`
	ResultsJSON string    `json:"resultsJson,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type FetchRecord struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	FetchedAt time.Time `json:"fetchedAt"`
}

type Store struct {
	db     *sql.DB
	dbPath string
	mu     sync.RWMutex
}

func NewStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("创建数据库所在目录失败: %w", err)
	}

	dsn := dbPath + sqliteDSNParams
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 数据库失败: %w", err)
	}

	s := &Store{db: db, dbPath: dbPath}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化数据库 Schema 失败: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS app_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS search_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		query TEXT NOT NULL,
		count INTEGER DEFAULT 10,
		need_content INTEGER DEFAULT 0,
		result_count INTEGER DEFAULT 0,
		results_json TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS fetch_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT NOT NULL,
		title TEXT,
		content TEXT,
		fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_search_created ON search_history(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_search_query ON search_history(query);
	CREATE INDEX IF NOT EXISTS idx_fetch_created ON fetch_history(fetched_at DESC);
	CREATE INDEX IF NOT EXISTS idx_fetch_url ON fetch_history(url);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// 渐进兼容迁移：对老数据库补充 need_content 列（若已存在则忽略错误）
	_, _ = s.db.Exec("ALTER TABLE search_history ADD COLUMN need_content INTEGER DEFAULT 0;")

	return nil
}

func (s *Store) GetConfig() (AppConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg := AppConfig{
		BaseURL:      "https://api.deepseek.com",
		DefaultCount: 10,
		NeedContent:  false,
	}

	rows, err := s.db.Query("SELECT key, value FROM app_settings")
	if err != nil {
		return cfg, err
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		switch k {
		case "api_key":
			cfg.APIKey = v
		case "base_url":
			if v != "" {
				cfg.BaseURL = v
			}
		case "default_count":
			var cnt int
			if _, err := fmt.Sscanf(v, "%d", &cnt); err == nil && cnt > 0 {
				cfg.DefaultCount = cnt
			}
		case "need_content":
			cfg.NeedContent = (v == "1" || strings.ToLower(v) == "true")
		case "updated_at":
			cfg.UpdatedAt = v
		}
	}
	return cfg, nil
}

func (s *Store) SaveConfig(cfg AppConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	upsertStmt := `
	INSERT INTO app_settings(key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP;
	`
	nowStr := time.Now().Format(time.RFC3339)
	pairs := map[string]string{
		"api_key":       cfg.APIKey,
		"base_url":      cfg.BaseURL,
		"default_count": fmt.Sprintf("%d", cfg.DefaultCount),
		"need_content":  fmt.Sprintf("%t", cfg.NeedContent),
		"updated_at":    nowStr,
	}

	for k, v := range pairs {
		if _, err := tx.Exec(upsertStmt, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AddSearchHistory(query string, count int, needContent bool, resultCount int, results interface{}) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var resultsJSON string
	if results != nil {
		data, _ := json.Marshal(results)
		resultsJSON = string(data)
	}

	needVal := 0
	if needContent {
		needVal = 1
	}

	res, err := s.db.Exec(`
		INSERT INTO search_history(query, count, need_content, result_count, results_json, created_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, query, count, needVal, resultCount, resultsJSON)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetCachedSearch 检查本地是否命中近期有效搜索缓存
func (s *Store) GetCachedSearch(query string, count int, needContent bool, maxAge time.Duration) (*SearchRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query = strings.TrimSpace(query)
	needVal := 0
	if needContent {
		needVal = 1
	}

	cutoff := dbCutoff(maxAge)

	row := s.db.QueryRow(`
		SELECT id, query, count, need_content, result_count, results_json, created_at
		FROM search_history
		WHERE query = ? AND need_content >= ? AND count >= ? AND created_at >= ? AND result_count > 0
		ORDER BY id DESC LIMIT 1
	`, query, needVal, count, cutoff)

	var r SearchRecord
	var createdAt string
	var resJSON sql.NullString
	var nc int
	if err := row.Scan(&r.ID, &r.Query, &r.Count, &nc, &r.ResultCount, &resJSON, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.NeedContent = (nc == 1)
	if resJSON.Valid {
		r.ResultsJSON = resJSON.String
	}
	r.CreatedAt, _ = parseDBTime(createdAt)

	// 宽缓存截断：历史记录允许 count 更宽（WHERE count >= ?），命中后统一把
	// 结果列表截到本次请求条数，保证“返回结果数 == 请求条数”的调用契约。
	if r.ResultsJSON != "" {
		r.ResultsJSON, r.ResultCount = truncateResultsJSON(r.ResultsJSON, count)
	}
	return &r, nil
}

// truncateResultsJSON 把 results 数组的 JSON 串截断为前 count 条；
// 解析失败或本就不足 count 条时原样返回，交由上层容错。
func truncateResultsJSON(raw string, count int) (string, int) {
	if count <= 0 || raw == "" {
		return raw, 0
	}
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err != nil || len(items) <= count {
		return raw, len(items)
	}
	trimmed, err := json.Marshal(items[:count])
	if err != nil {
		return raw, len(items)
	}
	return string(trimmed), count
}

func (s *Store) ListSearchHistory(limit int) ([]SearchRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.Query(`
		SELECT id, query, count, need_content, result_count, results_json, created_at
		FROM search_history
		ORDER BY id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SearchRecord
	for rows.Next() {
		var r SearchRecord
		var createdAt string
		var resJSON sql.NullString
		var nc int
		if err := rows.Scan(&r.ID, &r.Query, &r.Count, &nc, &r.ResultCount, &resJSON, &createdAt); err != nil {
			continue
		}
		r.NeedContent = (nc == 1)
		if resJSON.Valid {
			r.ResultsJSON = resJSON.String
		}
		r.CreatedAt, _ = parseDBTime(createdAt)
		records = append(records, r)
	}
	return records, nil
}

func (s *Store) AddFetchHistory(url, title, content string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`
		INSERT INTO fetch_history(url, title, content, fetched_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	`, url, title, content)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetCachedFetch 检查本地是否命中近期有效网页抓取缓存
func (s *Store) GetCachedFetch(url string, maxAge time.Duration) (*FetchRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	url = strings.TrimSpace(url)
	cutoff := dbCutoff(maxAge)

	row := s.db.QueryRow(`
		SELECT id, url, title, content, fetched_at
		FROM fetch_history
		WHERE url = ? AND fetched_at >= ? AND length(content) > 0
		ORDER BY id DESC LIMIT 1
	`, url, cutoff)

	var r FetchRecord
	var fetchedAt string
	if err := row.Scan(&r.ID, &r.URL, &r.Title, &r.Content, &fetchedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.FetchedAt, _ = parseDBTime(fetchedAt)
	return &r, nil
}

func (s *Store) ListFetchHistory(limit int) ([]FetchRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.Query(`
		SELECT id, url, title, content, fetched_at
		FROM fetch_history
		ORDER BY id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []FetchRecord
	for rows.Next() {
		var r FetchRecord
		var fetchedAt string
		if err := rows.Scan(&r.ID, &r.URL, &r.Title, &r.Content, &fetchedAt); err != nil {
			continue
		}
		r.FetchedAt, _ = parseDBTime(fetchedAt)
		records = append(records, r)
	}
	return records, nil
}
