package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"file-sync/ignore"
	"file-sync/models"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanDiffExecute(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	writeFile(t, filepath.Join(src, "a.txt"), "hello")
	writeFile(t, filepath.Join(src, "sub", "b.txt"), "world")
	writeFile(t, filepath.Join(src, "node_modules", "x.txt"), "junk")
	writeFile(t, filepath.Join(src, "debug.log"), "log")

	writeFile(t, filepath.Join(dst, "a.txt"), "old")
	writeFile(t, filepath.Join(dst, "stale", "old.txt"), "old")
	writeFile(t, filepath.Join(dst, "emptydir", "keep", "placeholder"), "x")

	m := ignore.NewMatcher([]string{"node_modules/", "*.log"})
	srcTree, err := ScanDirectory(src, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	dstTree, err := ScanDirectory(dst, m, nil)
	if err != nil {
		t.Fatal(err)
	}

	diff := ComputeDiff(srcTree, dstTree)
	if len(diff.Added) != 1 || diff.Added[0] != "sub/b.txt" {
		t.Errorf("Added = %v", diff.Added)
	}
	if len(diff.Modified) != 1 || diff.Modified[0] != "a.txt" {
		t.Errorf("Modified = %v", diff.Modified)
	}
	if len(diff.Deleted) != 2 {
		t.Errorf("Deleted = %v", diff.Deleted)
	}

	pt := NewProgressTracker("test")
	if errs := Execute(context.Background(), src, dst, diff, pt); len(errs) > 0 {
		t.Fatalf("execute errors: %v", errs)
	}
	if got := pt.Snapshot().Status; got != "completed" {
		t.Errorf("status = %q", got)
	}

	b, _ := os.ReadFile(filepath.Join(dst, "a.txt"))
	if string(b) != "hello" {
		t.Errorf("a.txt = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dst, "sub", "b.txt")); err != nil {
		t.Error("sub/b.txt not copied")
	}
	if _, err := os.Stat(filepath.Join(dst, "stale", "old.txt")); !os.IsNotExist(err) {
		t.Error("stale/old.txt not deleted")
	}
	if _, err := os.Stat(filepath.Join(dst, "stale")); !os.IsNotExist(err) {
		t.Error("empty stale dir not removed")
	}
	if _, err := os.Stat(filepath.Join(dst, "emptydir", "keep", "placeholder")); !os.IsNotExist(err) {
		t.Error("target-only file should be deleted")
	}
	if _, err := os.Stat(filepath.Join(dst, "emptydir")); !os.IsNotExist(err) {
		t.Error("empty emptydir should be removed")
	}
}

func TestExecuteCancel(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, filepath.Join(src, "f"+string(rune('0'+i))+".txt"), "data")
	}
	m := ignore.NewMatcher(nil)
	srcTree, _ := ScanDirectory(src, m, nil)
	diff := ComputeDiff(srcTree, &models.FileTree{Files: map[string]*models.FileInfo{}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pt := NewProgressTracker("test")
	errs := Execute(ctx, src, dst, diff, pt)
	if len(errs) == 0 {
		t.Fatal("expected cancel error")
	}
	if pt.Snapshot().Status != "error" {
		t.Errorf("status = %q", pt.Snapshot().Status)
	}
}
