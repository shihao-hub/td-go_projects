package core

import (
	"os"
	"path/filepath"
	"testing"

	"nutvault/internal/storage"
)

func TestBackupAndRestoreIntegration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nutvault_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	store, err := storage.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	nutstoreRoot := filepath.Join(tempDir, "nutstore")
	_ = store.SetConfig("nutstore_root", nutstoreRoot)

	engine := NewEngine(store)

	// 1. 创建源测试文件
	sourceFile := filepath.Join(tempDir, "source", "settings.json")
	_ = os.MkdirAll(filepath.Dir(sourceFile), 0o755)
	initialContent := []byte(`{"theme": "dark", "fontSize": 14}`)
	if err := os.WriteFile(sourceFile, initialContent, 0o644); err != nil {
		t.Fatal(err)
	}

	info := AnalyzePath(sourceFile)
	target, err := store.AddTarget(&storage.Target{
		SourcePath:      info.SourcePath,
		SourceDir:       info.SourceDir,
		FileName:        info.FileName,
		MappedDir:       info.MappedDir,
		ScheduleEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. 执行首次备份
	res1, err := engine.BackupTarget(target, false)
	if err != nil {
		t.Fatalf("第一次备份失败: %v", err)
	}
	if res1.Status != "success" {
		t.Fatalf("状态异常: %s", res1.Status)
	}
	if _, err := os.Stat(res1.FullNutstorePath); err != nil {
		t.Fatalf("坚果云备份文件不存在: %v", err)
	}

	// 3. 再次执行备份（内容未变），预期被智能跳过
	targetUpdated, _ := store.GetTargetByID(target.ID)
	res2, err := engine.BackupTarget(targetUpdated, false)
	if err != nil {
		t.Fatalf("第二次备份报错: %v", err)
	}
	if res2.Status != "skipped_unchanged" {
		t.Fatalf("期望跳过未修改，实际状态: %s", res2.Status)
	}

	// 4. 修改源文件并备份
	modifiedContent := []byte(`{"theme": "light", "fontSize": 16}`)
	_ = os.WriteFile(sourceFile, modifiedContent, 0o644)

	res3, err := engine.BackupTarget(targetUpdated, false)
	if err != nil {
		t.Fatalf("修改后备份报错: %v", err)
	}
	if res3.Status != "success" {
		t.Fatalf("修改后备份状态异常: %s", res3.Status)
	}

	// 5. 对比首次快照与当前文件差异
	diff, err := engine.CompareSnapshotWithSource(res1.SnapshotID)
	if err != nil {
		t.Fatalf("比对差异失败: %v", err)
	}
	if diff.Identical {
		t.Fatalf("内容应该不同")
	}

	// 6. 执行恢复首次快照
	restoreRes, err := engine.RestoreSnapshot(res1.SnapshotID, "", false)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	restoredData, _ := os.ReadFile(sourceFile)
	if string(restoredData) != string(initialContent) {
		t.Fatalf("恢复内容不匹配: got %s, want %s", string(restoredData), string(initialContent))
	}
	if restoreRes.PreRestorePath == "" {
		t.Fatalf("未生成恢复前安全副本")
	}
	if _, err := os.Stat(restoreRes.PreRestorePath); err != nil {
		t.Fatalf("恢复前安全副本不存在: %v", err)
	}
}
