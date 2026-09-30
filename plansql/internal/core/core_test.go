package core_test

import (
	"os"
	"path/filepath"
	"plansql/internal/core"
	"testing"
)

func TestStoreAndWAL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "plansql-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sqlPath := filepath.Join(tmpDir, "test_status.sql")
	wal := core.NewWALManager(sqlPath)

	store, err := core.NewMemoryStore()
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	// 1. 测试 AppendMutation
	validStatus := `{"state":"in_progress","progress":50}`
	sqlStr, err := wal.AppendMutation(store, "plan", "docs/plans/01-test.md", validStatus)
	if err != nil {
		t.Fatalf("append mutation failed: %v", err)
	}
	if sqlStr == "" {
		t.Fatalf("expected non-empty sql string")
	}

	// 2. 检查 Store 数据投影
	item, err := store.GetItemByPath("docs/plans/01-test.md")
	if err != nil {
		t.Fatalf("get item failed: %v", err)
	}
	if item == nil {
		t.Fatalf("expected item found")
	}
	if item.Type != "plan" || item.Status != validStatus {
		t.Errorf("unexpected item content: %+v", item)
	}

	// 3. 测试更新 UPSERT
	updatedStatus := `{"state":"completed","progress":100}`
	_, err = wal.AppendMutation(store, "plan", "docs/plans/01-test.md", updatedStatus)
	if err != nil {
		t.Fatalf("append update failed: %v", err)
	}

	itemUpdated, err := store.GetItemByPath("docs/plans/01-test.md")
	if err != nil || itemUpdated == nil {
		t.Fatalf("get updated item failed: %v", err)
	}
	if itemUpdated.Status != updatedStatus {
		t.Errorf("expected updated status, got: %s", itemUpdated.Status)
	}

	// 4. 测试 Check 校验
	checkRes, err := wal.Check()
	if err != nil {
		t.Fatalf("wal check failed: %v", err)
	}
	if !checkRes.Valid || len(checkRes.Errors) > 0 {
		t.Fatalf("expected check valid, got errors: %v", checkRes.Errors)
	}
	if checkRes.TotalStatements != 2 {
		t.Errorf("expected 2 statements, got %d", checkRes.TotalStatements)
	}

	// 5. 测试全新 store 重放还原
	replayedStore, err := core.NewMemoryStore()
	if err != nil {
		t.Fatalf("new replayed store: %v", err)
	}
	defer replayedStore.Close()

	if err := wal.Replay(replayedStore); err != nil {
		t.Fatalf("replay failed: %v", err)
	}

	replayedItem, err := replayedStore.GetItemByPath("docs/plans/01-test.md")
	if err != nil || replayedItem == nil {
		t.Fatalf("get replayed item failed: %v", err)
	}
	if replayedItem.Status != updatedStatus {
		t.Errorf("replayed status mismatch, got %s", replayedItem.Status)
	}

	// 6. 测试非法 JSON 拦截
	_, err = wal.AppendMutation(store, "plan", "docs/plans/bad.md", `not-a-json`)
	if err == nil {
		t.Fatalf("expected error on invalid JSON, got nil")
	}
}

func TestScanner(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "plansql-scan-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 创建目录树
	p1 := filepath.Join(tmpDir, "docs", "plans", "01-doc.md")
	p2 := filepath.Join(tmpDir, "projects", "sub", "specs", "02-spec.md")
	pIgnore := filepath.Join(tmpDir, "other", "readme.md")

	for _, p := range []string{p1, p2, pIgnore} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(p, []byte("# Test"), 0644); err != nil {
			t.Fatalf("write file failed: %v", err)
		}
	}

	store, err := core.NewMemoryStore()
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	// 先将 01-doc.md 登记入库，并登记一个悬空记录 dangling.md
	wal := core.NewWALManager(filepath.Join(tmpDir, "test.sql"))
	_, err = wal.AppendMutation(store, "plan", "docs/plans/01-doc.md", `{"state":"completed"}`)
	if err != nil {
		t.Fatalf("append p1 failed: %v", err)
	}
	_, err = wal.AppendMutation(store, "plan", "docs/plans/dangling.md", `{"state":"abandoned"}`)
	if err != nil {
		t.Fatalf("append dangling failed: %v", err)
	}

	scanner := core.NewScanner(tmpDir)
	report, err := scanner.Scan(store)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if report.TotalDiscovered != 2 {
		t.Errorf("expected 2 discovered (p1 and p2), got %d", report.TotalDiscovered)
	}
	if len(report.Aligned) != 1 || report.Aligned[0].Path != "docs/plans/01-doc.md" {
		t.Errorf("expected 1 aligned (p1), got %+v", report.Aligned)
	}
	if len(report.Unregistered) != 1 || report.Unregistered[0].Path != "projects/sub/specs/02-spec.md" {
		t.Errorf("expected 1 unregistered (p2), got %+v", report.Unregistered)
	}
	if len(report.Dangling) != 1 || report.Dangling[0].Path != "docs/plans/dangling.md" {
		t.Errorf("expected 1 dangling, got %+v", report.Dangling)
	}
}
