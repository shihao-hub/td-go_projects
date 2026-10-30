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

func TestCheckZedDBTitle(t *testing.T) {
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
			('sess_with_title', '分析acp标题代理项目', NULL),
			('sess_empty', '', NULL);
	`)
	if err != nil {
		t.Fatalf("failed to populate mock db: %v", err)
	}
	db.Close()

	// 场景 1：会话存在 title_override
	title, ok := CheckZedDBTitle("sess_with_override", dbPath)
	if !ok || title != "人工覆盖标题" {
		t.Errorf("expected '人工覆盖标题', got %q (ok=%v)", title, ok)
	}

	// 场景 2：会话存在 title
	title, ok = CheckZedDBTitle("sess_with_title", dbPath)
	if !ok || title != "分析acp标题代理项目" {
		t.Errorf("expected '分析acp标题代理项目', got %q (ok=%v)", title, ok)
	}

	// 场景 3：会话标题为空
	_, ok = CheckZedDBTitle("sess_empty", dbPath)
	if ok {
		t.Errorf("expected sess_empty to return false")
	}

	// 场景 4：会话不存在
	_, ok = CheckZedDBTitle("sess_not_found", dbPath)
	if ok {
		t.Errorf("expected sess_not_found to return false")
	}
}
