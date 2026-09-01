package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"file-sync-native/config"
	"file-sync-native/engine"
	"file-sync-native/models"
)

func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	cache := engine.NewHashCache(filepath.Join(t.TempDir(), "cache.gob"))
	return NewApp(cfg, cache), cfg.Path()
}

func waitNotRunning(t *testing.T, a *App, id string, timeout time.Duration) models.SyncProgress {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !a.isRunning(id) {
			return a.Progress(id)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s still running after %v", id, timeout)
	return models.SyncProgress{}
}

func waitStatus(t *testing.T, a *App, id string, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if a.Progress(id).Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s status %s not reached, got %s", id, want, a.Progress(id).Status)
}

func TestTaskCRUDAndValidation(t *testing.T) {
	a, cfgPath := newTestApp(t)

	task, err := a.CreateTask(TaskInput{Name: "t1", SourcePath: `C:\a`, TargetPath: `D:\b`, IgnoreRules: []string{"*.log"}})
	if err != nil {
		t.Fatal(err)
	}
	if task.Name != "t1" || !task.Enabled {
		t.Fatalf("created task: %+v", task)
	}

	// 名称为空时自动命名
	task2, err := a.CreateTask(TaskInput{SourcePath: `C:\x\y`, TargetPath: `D:\z`})
	if err != nil {
		t.Fatal(err)
	}
	if task2.Name == "" {
		t.Fatal("name should be auto-generated")
	}

	// 非法路径组合
	if _, err := a.CreateTask(TaskInput{SourcePath: `C:\a`, TargetPath: `C:\a`}); err == nil {
		t.Fatal("same path should be rejected")
	}
	if _, err := a.CreateTask(TaskInput{SourcePath: `C:\a`, TargetPath: `C:\a\sub`}); err == nil {
		t.Fatal("nested target should be rejected")
	}
	// 目标目录冲突
	if _, err := a.CreateTask(TaskInput{Name: "x", SourcePath: `C:\other`, TargetPath: `D:\b`}); err == nil {
		t.Fatal("duplicate target should be rejected")
	}

	// 更新与删除
	if _, err := a.UpdateTask(task.ID, TaskInput{Name: "t1-rename", SourcePath: `C:\a`, TargetPath: `D:\b2`}); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatal("config should be persisted")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	a, _ := newTestApp(t)
	got, err := a.UpdateSettings(`C:\Backup`)
	if err != nil {
		t.Fatal(err)
	}
	if got.BackupRoot != `C:\Backup` {
		t.Fatalf("settings: %+v", got)
	}
	if s := a.GetSettings(); s.BackupRoot != `C:\Backup` {
		t.Fatalf("get: %+v", s)
	}
}

func TestSyncFlowWithDeleteDeclinedAndApproved(t *testing.T) {
	a, _ := newTestApp(t)
	src, dst := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, "keep.txt"), []byte("K"), 0o644)
	os.WriteFile(filepath.Join(src, "drop.txt"), []byte("D"), 0o644)

	task, err := a.CreateTask(TaskInput{SourcePath: src, TargetPath: dst})
	if err != nil {
		t.Fatal(err)
	}

	// 首次同步
	if err := a.StartSync(task.ID, false); err != nil {
		t.Fatal(err)
	}
	p := waitNotRunning(t, a, task.ID, 5*time.Second)
	if p.Status != models.StatusCompleted {
		t.Fatalf("first sync status: %s (%s)", p.Status, p.ErrorMessage)
	}
	if _, err := os.Stat(filepath.Join(dst, "keep.txt")); err != nil {
		t.Fatal("keep.txt should be copied")
	}

	// ScanTask 干跑
	os.Remove(filepath.Join(src, "drop.txt"))
	diff, err := a.ScanTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Deleted) != 1 || diff.Deleted[0] != "drop.txt" {
		t.Fatalf("dry-run diff: %+v", diff)
	}
	if _, err := os.Stat(filepath.Join(dst, "drop.txt")); err != nil {
		t.Fatal("dry-run must not delete")
	}

	// 正式同步：等待删除确认 → 拒绝
	if err := a.StartSync(task.ID, false); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, a, task.ID, models.StatusAwaitingDelete, 5*time.Second)
	if _, err := os.Stat(filepath.Join(dst, "drop.txt")); err != nil {
		t.Fatal("must not delete before confirmation")
	}
	if err := a.ConfirmDeletes(task.ID, false); err != nil {
		t.Fatal(err)
	}
	p = waitNotRunning(t, a, task.ID, 5*time.Second)
	if p.Status != models.StatusCompleted {
		t.Fatalf("declined sync status: %s", p.Status)
	}
	if _, err := os.Stat(filepath.Join(dst, "drop.txt")); err != nil {
		t.Fatal("declined: file must survive")
	}

	// 再次同步：确认删除
	if err := a.StartSync(task.ID, false); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, a, task.ID, models.StatusAwaitingDelete, 5*time.Second)
	if err := a.ConfirmDeletes(task.ID, true); err != nil {
		t.Fatal(err)
	}
	waitNotRunning(t, a, task.ID, 5*time.Second)
	if _, err := os.Stat(filepath.Join(dst, "drop.txt")); !os.IsNotExist(err) {
		t.Fatal("approved: file should be deleted")
	}
}

func TestCancelSync(t *testing.T) {
	a, _ := newTestApp(t)
	src, dst := t.TempDir(), t.TempDir()
	for i := 0; i < 100; i++ {
		os.WriteFile(filepath.Join(src, "f.txt"), []byte("payload"), 0o644)
	}
	task, err := a.CreateTask(TaskInput{SourcePath: src, TargetPath: dst})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.StartSync(task.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := a.CancelSync(task.ID); err != nil {
		t.Fatal(err)
	}
	p := waitNotRunning(t, a, task.ID, 5*time.Second)
	if p.Status != models.StatusCancelled {
		t.Fatalf("want cancelled, got %s", p.Status)
	}
	// 取消后可再次启动
	if err := a.StartSync(task.ID, false); err != nil {
		t.Fatalf("restart after cancel: %v", err)
	}
	waitNotRunning(t, a, task.ID, 5*time.Second)
}

func TestSyncRejectsInvalidSource(t *testing.T) {
	a, _ := newTestApp(t)
	task, err := a.CreateTask(TaskInput{SourcePath: t.TempDir(), TargetPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(task.SourcePath)
	if err := a.StartSync(task.ID, false); err == nil {
		t.Fatal("missing source should be rejected")
	}
}
