// store_test.go：配置、会话记录与命令历史的落盘往返。
package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cfg := Config{
		Entries: []Entry{{ID: "e1", Name: "项目", WorkDir: `C:\work`, Command: "dir"}},
		Groups:  []Group{{ID: "g1", Name: "默认组", EntryIDs: []string{"e1"}}},
	}
	if err := st.ReplaceConfig(cfg); err != nil {
		t.Fatalf("ReplaceConfig: %v", err)
	}

	reopened, err := New(dir)
	if err != nil {
		t.Fatalf("New again: %v", err)
	}
	got := reopened.Config()
	if len(got.Entries) != 1 || got.Entries[0].Name != "项目" {
		t.Fatalf("entries = %+v", got.Entries)
	}
	if len(got.Groups) != 1 || got.Groups[0].EntryIDs[0] != "e1" {
		t.Fatalf("groups = %+v", got.Groups)
	}
	if _, ok := reopened.Entry("e1"); !ok {
		t.Fatal("Entry 查找失败")
	}
	if _, ok := reopened.Group("g1"); !ok {
		t.Fatal("Group 查找失败")
	}
}

func TestSessionRecordRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	rec := SessionRecord{
		ID: "s1", WorkDir: `C:\work`, StartedAt: now, ExitedAt: now.Add(time.Second),
		Status: "exited", ExitCode: 1, Commands: []string{"dir", "git status"},
		LastOutput: "error",
	}
	if err := st.AppendSession(rec); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Sessions()
	if len(got) != 1 || got[0].Commands[1] != "git status" {
		t.Fatalf("sessions = %+v", got)
	}
	if rec, ok := reopened.Session("s1"); !ok || rec.LastOutput != "error" {
		t.Fatalf("Session = %+v ok=%v", rec, ok)
	}
}

func TestHistoryPrepareAndAppend(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := st.History()
	if err := h.Prepare("s1"); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	path := h.SessionPath("s1")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("新会话历史应为空，got %q", data)
	}
	if err := os.WriteFile(path, []byte("dir\ngit status\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := h.ReadSession("s1"); len(got) != 2 || got[1] != "git status" {
		t.Fatalf("ReadSession = %v", got)
	}
	if err := h.AppendGlobal(h.ReadSession("s1")); err != nil {
		t.Fatal(err)
	}
	global := h.Global()
	if len(global) < 2 || global[len(global)-1] != "git status" {
		t.Fatalf("Global = %v", global)
	}
}

func TestWriteLinesNoBOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.txt")
	if err := writeLines(path, []string{"你好", "dir"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(string(data), "\uFEFF") {
		t.Fatal("history 不应带 BOM")
	}
	if !strings.Contains(string(data), "你好") {
		t.Fatalf("data = %q", data)
	}
}

func TestDeleteSession(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendSession(SessionRecord{ID: "s1", WorkDir: `C:\a`}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendSession(SessionRecord{ID: "s2", WorkDir: `C:\b`}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSession("s1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Session("s1"); ok {
		t.Fatal("s1 应已删除")
	}
	if _, ok := reopened.Session("s2"); !ok {
		t.Fatal("s2 应保留")
	}
	if err := reopened.DeleteSession("missing"); err == nil {
		t.Fatal("删除不存在记录应报错")
	}
}
