package core

import (
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// SchemaDDL 是 plan_spec_status 投影表的初始化定义
const SchemaDDL = `
CREATE TABLE IF NOT EXISTS plan_spec_status (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL CHECK(type IN ('plan', 'spec')),
    path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK(json_valid(status)),
    created_at TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
);
CREATE INDEX IF NOT EXISTS idx_plan_spec_path ON plan_spec_status(path);
CREATE INDEX IF NOT EXISTS idx_plan_spec_type ON plan_spec_status(type);
`

// Item 表示一条 Plan 或 Spec 的状态记录
type Item struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Path      string `json:"path"`
	Status    string `json:"status"` // JSON 字符串
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Store 管理 SQLite 内存/缓存投影视图
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewMemoryStore 创建纯内存的 SQLite 投影 Store
func NewMemoryStore() (*Store, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open sqlite memory: %w", err)
	}

	// 限制单连接保证内存模式数据连续性
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(SchemaDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("exec schema ddl: %w", err)
	}

	return &Store{db: db}, nil
}

// Close 关闭数据库
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// ExecDirect 直接执行 SQL 语句并更新投影
func (s *Store) ExecDirect(query string, args ...any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(query, args...)
	return err
}

// GetAllItems 获取全部记录，遵循禁 SELECT * 原则显式指定列
func (s *Store) GetAllItems() ([]Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT id, type, path, status, created_at, updated_at FROM plan_spec_status ORDER BY path ASC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Type, &it.Path, &it.Status, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// GetItemByPath 按 path 获取单条记录
func (s *Store) GetItemByPath(path string) (*Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT id, type, path, status, created_at, updated_at FROM plan_spec_status WHERE path = ? LIMIT 1`
	var it Item
	err := s.db.QueryRow(query, path).Scan(&it.ID, &it.Type, &it.Path, &it.Status, &it.CreatedAt, &it.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get item by path: %w", err)
	}
	return &it, nil
}
