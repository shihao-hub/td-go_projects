package zedthreads

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// newTestDB 建一个最小可用的 sidebar_threads（只含迁移用到的列）。
func newTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE sidebar_threads (
		thread_id BLOB PRIMARY KEY,
		session_id TEXT,
		agent_id TEXT,
		title TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		created_at TEXT,
		folder_paths TEXT,
		folder_paths_order TEXT,
		archived INTEGER DEFAULT 0,
		main_worktree_paths TEXT,
		main_worktree_paths_order TEXT,
		remote_connection TEXT,
		interacted_at TEXT,
		title_override TEXT
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db, path
}

func insertThread(t *testing.T, db *sql.DB, id byte, agent, sid string, archived int) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO sidebar_threads (thread_id, session_id, agent_id, title, updated_at, archived) VALUES (?,?,?,?,?,?)",
		[]byte{id}, sid, agent, "t", "2026-10-31T00:00:00Z", archived,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestPlanAdoptSkipsArchivedAndEmpty(t *testing.T) {
	db, _ := newTestDB(t)
	insertThread(t, db, 1, "antigravity-acp", "sid-1", 0)
	insertThread(t, db, 2, "antigravity-acp", "sid-2", 0)
	insertThread(t, db, 3, "antigravity-acp", "sid-3", 1) // 已归档
	insertThread(t, db, 4, "antigravity-acp", "", 0)      // 空 thread
	insertThread(t, db, 5, "other", "sid-5", 0)           // 其它 agent

	plan, err := PlanAdopt(db, "antigravity-acp", "proxy", false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Rows) != 2 {
		t.Fatalf("rows=%d, want 2", len(plan.Rows))
	}
	if plan.Skipped != 1 {
		t.Errorf("skipped=%d, want 1", plan.Skipped)
	}
	if plan.EmptySID != 1 {
		t.Errorf("emptySID=%d, want 1", plan.EmptySID)
	}

	all, err := PlanAdopt(db, "antigravity-acp", "proxy", true)
	if err != nil {
		t.Fatalf("plan all: %v", err)
	}
	if len(all.Rows) != 3 {
		t.Errorf("include-archived rows=%d, want 3", len(all.Rows))
	}
}

func TestPlanAdoptRejectsSameAndEmpty(t *testing.T) {
	db, _ := newTestDB(t)
	if _, err := PlanAdopt(db, "a", "a", false); err == nil {
		t.Error("expected error for same from/to")
	}
	if _, err := PlanAdopt(db, "", "b", false); err == nil {
		t.Error("expected error for empty from")
	}
}

func TestApplyTxAdopt(t *testing.T) {
	db, _ := newTestDB(t)
	insertThread(t, db, 1, "antigravity-acp", "sid-1", 0)
	insertThread(t, db, 2, "antigravity-acp", "sid-2", 0)

	plan, err := PlanAdopt(db, "antigravity-acp", "proxy", false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	n, err := applyTx(db, plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if n != 2 {
		t.Fatalf("updated=%d, want 2", n)
	}

	var left int
	if err := db.QueryRow("SELECT COUNT(*) FROM sidebar_threads WHERE agent_id='antigravity-acp'").Scan(&left); err != nil {
		t.Fatalf("count: %v", err)
	}
	if left != 0 {
		t.Errorf("antigravity-acp rows=%d, want 0", left)
	}
	var moved int
	if err := db.QueryRow("SELECT COUNT(*) FROM sidebar_threads WHERE agent_id='proxy'").Scan(&moved); err != nil {
		t.Fatalf("count: %v", err)
	}
	if moved != 2 {
		t.Errorf("proxy rows=%d, want 2", moved)
	}
}

func TestPlanArchiveOnlyActive(t *testing.T) {
	db, _ := newTestDB(t)
	insertThread(t, db, 1, "x", "sid-1", 0)
	insertThread(t, db, 2, "x", "sid-2", 1)
	insertThread(t, db, 3, "x", "sid-3", 0)

	plan, err := PlanArchive(db, "x")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Rows) != 2 {
		t.Fatalf("rows=%d, want 2", len(plan.Rows))
	}
	n, err := applyTx(db, plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if n != 2 {
		t.Fatalf("updated=%d, want 2", n)
	}

	var active int
	if err := db.QueryRow("SELECT COUNT(*) FROM sidebar_threads WHERE archived=0").Scan(&active); err != nil {
		t.Fatalf("count: %v", err)
	}
	if active != 0 {
		t.Errorf("active=%d, want 0", active)
	}
}

func TestListFilters(t *testing.T) {
	db, _ := newTestDB(t)
	insertThread(t, db, 1, "a", "sid-1", 0)
	insertThread(t, db, 2, "a", "sid-2", 1)
	insertThread(t, db, 3, "b", "sid-3", 0)

	active, err := List(db, ListOptions{Agent: "a", Archived: "active"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("active=%d, want 1", len(active))
	}

	all, err := List(db, ListOptions{Archived: "all"})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all=%d, want 3", len(all))
	}

	if _, err := List(db, ListOptions{Archived: "bogus"}); err == nil {
		t.Error("expected error for invalid archived value")
	}
}
