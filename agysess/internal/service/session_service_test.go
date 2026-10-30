package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agysess/internal/model"

	_ "modernc.org/sqlite"
)

func createTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "conversation_summaries.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	defer db.Close()

	schema := `
	CREATE TABLE conversation_summaries (
		conversation_id text PRIMARY KEY,
		title text NOT NULL DEFAULT "",
		preview text NOT NULL DEFAULT "",
		step_count integer NOT NULL DEFAULT 0,
		last_modified_time datetime NOT NULL,
		workspace_uris text NOT NULL,
		status text NOT NULL DEFAULT "",
		source text NOT NULL DEFAULT "",
		project_id text NOT NULL DEFAULT "",
		agent_name text NOT NULL DEFAULT "",
		parent_conversation_id text NOT NULL DEFAULT "",
		nesting_depth integer NOT NULL DEFAULT 0,
		battle_id text NOT NULL DEFAULT "",
		winning_conversation_id text NOT NULL DEFAULT "",
		not_fully_idle numeric NOT NULL DEFAULT false,
		killed numeric NOT NULL DEFAULT false,
		last_user_input_time datetime NOT NULL DEFAULT "0001-01-01 00:00:00",
		last_user_input_step_index integer NOT NULL DEFAULT -1,
		app_data_dir text NOT NULL DEFAULT "",
		raw_summary blob,
		group_id text NOT NULL DEFAULT ""
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("创建表结构失败: %v", err)
	}

	insertSQL := `
	INSERT INTO conversation_summaries (
		conversation_id, title, preview, step_count, last_modified_time, workspace_uris, project_id, last_user_input_time
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`

	now := time.Now().Format(time.RFC3339)
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	_, err = db.Exec(insertSQL, "sess-1", "Go CLI Tool", "/test prompt", 10, past, "D:/proj1", "proj-a", past)
	if err != nil {
		t.Fatalf("插入 sess-1 失败: %v", err)
	}
	_, err = db.Exec(insertSQL, "sess-2", "Python Refactor", "/refactor", 25, now, "D:/proj2", "proj-b", now)
	if err != nil {
		t.Fatalf("插入 sess-2 失败: %v", err)
	}

	return dbPath
}

func TestSessionService_ListAndArchive(t *testing.T) {
	dbPath := createTestDB(t)
	metaPath := filepath.Join(t.TempDir(), "sessions_meta.json")

	svc, err := NewSessionService(dbPath, metaPath)
	if err != nil {
		t.Fatalf("初始化 SessionService 失败: %v", err)
	}

	ctx := context.Background()

	// 1. 默认查询活跃列表（应查出 2 条）
	sessions, stats, err := svc.ListSessions(ctx, model.FilterOptions{})
	if err != nil {
		t.Fatalf("ListSessions 失败: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("预期 2 条会话，实际得到 %d 条", len(sessions))
	}
	if stats.Total != 2 || stats.Active != 2 || stats.Archived != 0 {
		t.Fatalf("统计数据不符: %+v", stats)
	}

	// 2. 归档 sess-1
	if err := svc.ArchiveSession(ctx, "sess-1"); err != nil {
		t.Fatalf("ArchiveSession 失败: %v", err)
	}

	// 3. 再次默认查询（默认隐藏已归档），应只剩 sess-2
	sessions, stats, err = svc.ListSessions(ctx, model.FilterOptions{})
	if err != nil {
		t.Fatalf("ListSessions 失败: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "sess-2" {
		t.Fatalf("归档后默认查询异常: %+v", sessions)
	}
	if stats.Archived != 1 || stats.Active != 1 {
		t.Fatalf("归档后统计异常: %+v", stats)
	}

	// 4. 查询已归档会话
	archivedSessions, _, err := svc.ListSessions(ctx, model.FilterOptions{ArchivedOnly: true})
	if err != nil {
		t.Fatalf("ListSessions ArchivedOnly 失败: %v", err)
	}
	if len(archivedSessions) != 1 || archivedSessions[0].ID != "sess-1" {
		t.Fatalf("查询已归档会话异常: %+v", archivedSessions)
	}

	// 5. 更新备注与自定义标题
	if err := svc.UpdateNotes(ctx, "sess-1", "我的测试备注"); err != nil {
		t.Fatalf("UpdateNotes 失败: %v", err)
	}
	if err := svc.UpdateCustomTitle(ctx, "sess-1", "自定义标题1"); err != nil {
		t.Fatalf("UpdateCustomTitle 失败: %v", err)
	}

	detail, err := svc.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession 失败: %v", err)
	}
	if detail.CustomNotes != "我的测试备注" || detail.CustomTitle != "自定义标题1" {
		t.Fatalf("元数据更新未生效: %+v", detail)
	}
	if detail.DisplayTitle() != "自定义标题1" {
		t.Fatalf("DisplayTitle 预期使用自定义标题，实际得到: %s", detail.DisplayTitle())
	}

	// 6. 取消归档
	if err := svc.UnarchiveSession(ctx, "sess-1"); err != nil {
		t.Fatalf("UnarchiveSession 失败: %v", err)
	}
	sessions, stats, err = svc.ListSessions(ctx, model.FilterOptions{})
	if err != nil {
		t.Fatalf("ListSessions 失败: %v", err)
	}
	if len(sessions) != 2 || stats.Archived != 0 {
		t.Fatalf("取消归档后异常: len=%d, stats=%+v", len(sessions), stats)
	}

	_ = os.Remove(dbPath)
}
