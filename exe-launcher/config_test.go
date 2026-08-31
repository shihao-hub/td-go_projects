package main

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	return filepath.Join(dir, "exe-launcher", "config.json")
}

func TestConfigRoundTrip(t *testing.T) {
	withTempConfigDir(t)
	orig := &config{
		Entries: []Entry{
			{Name: "agent-reaper", Path: `C:\x\agent-reaper.exe`, AddedAt: "2026-01-01T00:00:00Z", Valid: true},
			{Name: "old", Path: `C:\gone\old.exe`, AddedAt: "2026-01-02T00:00:00Z"},
		},
		LastScanDir: `C:\WorkingProjects`,
	}
	if err := saveConfig(orig); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	got := loadConfig()
	if got.LastScanDir != orig.LastScanDir {
		t.Errorf("LastScanDir = %q, want %q", got.LastScanDir, orig.LastScanDir)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("Entries 数量 = %d, want 2", len(got.Entries))
	}
	if got.Entries[0].Name != "agent-reaper" || got.Entries[0].Path != `C:\x\agent-reaper.exe` ||
		got.Entries[0].AddedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("Entries[0] 往返不一致: %+v", got.Entries[0])
	}
	if got.Entries[0].Valid {
		t.Errorf("Valid 是运行时字段，不应落盘")
	}
}

func TestLoadConfigMissingOrBroken(t *testing.T) {
	p := withTempConfigDir(t)

	c := loadConfig() // 文件不存在
	if c == nil || len(c.Entries) != 0 || c.LastScanDir != "" {
		t.Errorf("缺文件时应返回空配置: %+v", c)
	}

	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	c = loadConfig() // 非法 JSON
	if len(c.Entries) != 0 {
		t.Errorf("损坏文件时应返回空配置: %+v", c)
	}
}

func TestStoreAddDedup(t *testing.T) {
	s := newStore(nil)
	if !s.Add("a", `C:\Tools\A.exe`) {
		t.Error("首次添加应成功")
	}
	if s.Add("a2", `c:\tools\a.exe`) {
		t.Error("大小写不同不应重复添加")
	}
	if s.Add("a3", `C:\Tools\.\A.exe`) {
		t.Error("等效路径不应重复添加")
	}
	if len(s.entries) != 1 {
		t.Fatalf("去重后应剩 1 条，实际 %d", len(s.entries))
	}
	if s.entries[0].Name != "a" {
		t.Errorf("显式名称应优先: %q", s.entries[0].Name)
	}
}

func TestStoreNewStoreDedup(t *testing.T) {
	s := newStore([]Entry{
		{Path: `C:\A\B.exe`},
		{Path: `c:\a\b.exe`},
		{Path: ""},
		{Path: `C:\C\C.exe`},
	})
	if len(s.entries) != 2 {
		t.Fatalf("载入去重后应剩 2 条，实际 %d", len(s.entries))
	}
}

func TestStoreAddDefaultName(t *testing.T) {
	s := newStore(nil)
	if !s.Add("", `C:\x\Foo.EXE`) {
		t.Fatal("添加应成功")
	}
	if s.entries[0].Name != "Foo" {
		t.Errorf("默认名称 = %q, want Foo", s.entries[0].Name)
	}
	if s.entries[0].AddedAt == "" {
		t.Error("AddedAt 应写入时间戳")
	}
}

func TestStoreRefreshAndClean(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.exe")
	if err := os.WriteFile(ok, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "gone.exe")

	s := newStore([]Entry{{Path: ok}, {Path: gone}})
	s.RefreshValid()
	if !s.entries[0].Valid {
		t.Error("存在的文件应为有效")
	}
	if s.entries[1].Valid {
		t.Error("不存在的文件应为失效")
	}
	if n := s.InvalidCount(); n != 1 {
		t.Fatalf("InvalidCount = %d, want 1", n)
	}
	if n := s.RemoveInvalid(); n != 1 {
		t.Fatalf("RemoveInvalid 剔除 %d, want 1", n)
	}
	if len(s.entries) != 1 || s.entries[0].Path != ok {
		t.Fatalf("清理后应只剩存在的条目: %+v", s.entries)
	}
}

func TestStoreRemove(t *testing.T) {
	s := newStore([]Entry{{Path: `C:\A.exe`}, {Path: `C:\B.exe`}})
	s.Remove(5)  // 越界忽略
	s.Remove(-1) // 越界忽略
	s.Remove(0)
	if len(s.entries) != 1 || s.entries[0].Path != `C:\B.exe` {
		t.Fatalf("Remove 后剩余不符: %+v", s.entries)
	}
}
