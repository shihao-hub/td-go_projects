package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestResearchMigrationBackupAndExpiredLease(t *testing.T) {
	root, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(root, "language_projects", "deepseeksearch", "dev", "tests")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "migration-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "legacy.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec("CREATE TABLE legacy_marker(value TEXT); INSERT INTO legacy_marker(value) VALUES('preserved')")
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backups, err := filepath.Glob(filepath.Join(dir, "deepseeksearch-pre-research-*.db"))
	if err != nil || len(backups) != 1 {
		t.Fatal("迁移备份不存在")
	}
	backup, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var marker string
	if err := backup.QueryRow("SELECT value FROM legacy_marker").Scan(&marker); err != nil || marker != "preserved" {
		t.Fatal("备份丢失原数据")
	}
	second, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	backups, _ = filepath.Glob(filepath.Join(dir, "deepseeksearch-pre-research-*.db"))
	if len(backups) != 1 {
		t.Fatal("重复启动再次备份")
	}
	ctx := context.Background()
	if err := store.CreateResearch(ctx, "r", "问题", "{}"); err != nil {
		t.Fatal(err)
	}
	if err := store.ClaimResearch(ctx, "r", "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("UPDATE research_sessions SET lease_until=0 WHERE id='r'"); err != nil {
		t.Fatal(err)
	}
	if err := second.ClaimResearch(ctx, "r", "new"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveResearch(ctx, "r", "old", "{}"); err != ErrResearchBusy {
		t.Fatal("过期持有者覆盖新租约")
	}
	if err := store.ReleaseResearch(ctx, "r", "old"); err != nil {
		t.Fatal(err)
	}
	if err := second.SaveResearch(ctx, "r", "new", "{}"); err != nil {
		t.Fatal("旧持有者解除了新租约")
	}
}
