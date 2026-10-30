package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestStorePersistence(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "sub", "titled_sessions.json")

	// 1. 首次运行：创建并登记
	s1, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	if s1.IsTitled("sess_123") {
		t.Errorf("expected sess_123 not titled")
	}

	if err := s1.MarkTitled("sess_123", "测试会话标题"); err != nil {
		t.Fatalf("MarkTitled failed: %v", err)
	}

	if !s1.IsTitled("sess_123") {
		t.Errorf("expected sess_123 to be titled")
	}

	// 2. 第二次运行：从磁盘加载，模拟进程重启
	s2, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore reload failed: %v", err)
	}

	if !s2.IsTitled("sess_123") {
		t.Errorf("expected sess_123 to remain titled after reload")
	}

	title, ok := s2.GetTitle("sess_123")
	if !ok || title != "测试会话标题" {
		t.Errorf("expected title '测试会话标题', got %q (ok=%v)", title, ok)
	}

	if s2.IsTitled("sess_unknown") {
		t.Errorf("expected sess_unknown not titled")
	}
}

func TestGetZedDBTitles(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mock_zed.sqlite")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE sidebar_threads (
			session_id TEXT,
			title TEXT,
			title_override TEXT
		);
		INSERT INTO sidebar_threads (session_id, title, title_override) VALUES
			('sess_with_override', '原始标题', '人工覆盖标题'),
			('sess_with_title', '这部分是如何实现的？', NULL),
			('sess_empty', '', NULL);
	`)
	if err != nil {
		t.Fatalf("failed to populate mock db: %v", err)
	}
	db.Close()

	// 场景 1：会话存在 title 与 title_override
	title, override, found := GetZedDBTitles("sess_with_override", dbPath)
	if !found || title != "原始标题" || override != "人工覆盖标题" {
		t.Errorf("unexpected result: title=%q override=%q found=%v", title, override, found)
	}

	// 场景 2：只有 title（是否占位由调用方结合 prompt 判定）
	title, override, found = GetZedDBTitles("sess_with_title", dbPath)
	if !found || title != "这部分是如何实现的？" || override != "" {
		t.Errorf("unexpected result: title=%q override=%q found=%v", title, override, found)
	}

	// 场景 3：标题均为空
	title, override, found = GetZedDBTitles("sess_empty", dbPath)
	if !found || title != "" || override != "" {
		t.Errorf("unexpected result: title=%q override=%q found=%v", title, override, found)
	}

	// 场景 4：会话不存在
	if _, _, found = GetZedDBTitles("sess_not_found", dbPath); found {
		t.Errorf("expected sess_not_found to return found=false")
	}
}
