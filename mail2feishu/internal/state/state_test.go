package state

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := New(path)
	if _, ok := store.LastUID("u@qq.com", 1); ok {
		t.Fatal("空存储不应返回水位线")
	}

	if err := store.Save("u@qq.com", 42, 1234); err != nil {
		t.Fatalf("Save() 错误: %v", err)
	}

	reloaded, err := Open(path)
	if err != nil {
		t.Fatalf("Open() 错误: %v", err)
	}
	uid, ok := reloaded.LastUID("u@qq.com", 42)
	if !ok || uid != 1234 {
		t.Fatalf("LastUID() = %d, %v; want 1234, true", uid, ok)
	}
	snap := reloaded.Snapshot()
	if snap.Version != schemaVersion || snap.Username != "u@qq.com" {
		t.Errorf("Snapshot = %+v", snap)
	}
	if snap.UpdatedAt.IsZero() {
		t.Error("UpdatedAt 不应为零值")
	}
}

func TestOpenMissingFile(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("文件不存在不应报错: %v", err)
	}
	if _, ok := store.LastUID("u", 1); ok {
		t.Fatal("文件不存在时不应返回水位线")
	}
}

func TestOpenCorruptFileStillUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("写入损坏文件失败: %v", err)
	}

	store, err := Open(path)
	if err == nil {
		t.Fatal("损坏文件应返回错误供调用方告警")
	}
	if _, ok := store.LastUID("u", 1); ok {
		t.Fatal("损坏状态不应返回水位线")
	}
	if err := store.Save("u", 1, 10); err != nil {
		t.Fatalf("损坏后仍应能重新写入: %v", err)
	}
	uid, ok := store.LastUID("u", 1)
	if !ok || uid != 10 {
		t.Fatalf("重写后 LastUID() = %d, %v", uid, ok)
	}
}

func TestLastUIDRequiresMatchingIdentity(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save("u@qq.com", 42, 123); err != nil {
		t.Fatalf("Save() 错误: %v", err)
	}
	if _, ok := store.LastUID("u@qq.com", 43); ok {
		t.Error("UIDVALIDITY 不匹配时应视为无效")
	}
	if _, ok := store.LastUID("other@qq.com", 42); ok {
		t.Error("账号不匹配时应视为无效")
	}
	if uid, ok := store.LastUID("u@qq.com", 42); !ok || uid != 123 {
		t.Errorf("匹配时应返回 123, got %d, %v", uid, ok)
	}
}

func TestZeroLastUIDIsInvalid(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save("u", 1, 0); err != nil {
		t.Fatalf("Save() 错误: %v", err)
	}
	if _, ok := store.LastUID("u", 1); ok {
		t.Fatal("UID 0 不是有效水位线")
	}
}

func TestSaveLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := New(path)
	if err := store.Save("u", 1, 5); err != nil {
		t.Fatalf("Save() 错误: %v", err)
	}
	matches, err := filepath.Glob(path + ".tmp")
	if err != nil {
		t.Fatalf("Glob() 错误: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("不应残留临时文件: %v", matches)
	}
}

func TestConcurrentSave(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "state.json"))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(uid uint32) {
			defer wg.Done()
			if err := store.Save("u", 1, uid); err != nil {
				t.Errorf("并发 Save() 错误: %v", err)
			}
		}(uint32(i + 1))
	}
	wg.Wait()
	if _, ok := store.LastUID("u", 1); !ok {
		t.Fatal("并发写入后应存在有效水位线")
	}
}
