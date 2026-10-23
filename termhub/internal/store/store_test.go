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

func TestTerminalSlotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.AppendTerminal(`C:\work`, "manual")
	if err != nil {
		t.Fatalf("AppendTerminal: %v", err)
	}
	second, err := st.AppendTerminal(`D:\proj`, "entry:e1")
	if err != nil {
		t.Fatalf("AppendTerminal: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("slot ID 应唯一")
	}
	if first.AutoStart || second.AutoStart {
		t.Fatal("新建 slot 默认不应自动重开")
	}

	if err := st.UpdateTerminalAutoStart(second.ID, true); err != nil {
		t.Fatalf("UpdateTerminalAutoStart: %v", err)
	}
	if err := st.UpdateTerminalLastSession(first.ID, "s001-1"); err != nil {
		t.Fatalf("UpdateTerminalLastSession: %v", err)
	}

	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	slots := reopened.Terminals()
	if len(slots) != 2 || slots[0].ID != first.ID || slots[1].ID != second.ID {
		t.Fatalf("terminals = %+v（应按创建顺序恢复）", slots)
	}
	if slots[0].WorkDir != `C:\work` || slots[0].LastSessionID != "s001-1" {
		t.Fatalf("slot[0] = %+v", slots[0])
	}
	if slots[1].WorkDir != `D:\proj` || slots[1].Source != "entry:e1" || !slots[1].AutoStart {
		t.Fatalf("slot[1] = %+v", slots[1])
	}
	if _, ok := reopened.TerminalByID(second.ID); !ok {
		t.Fatal("TerminalByID 查找失败")
	}

	if err := reopened.DeleteTerminal(first.ID); err != nil {
		t.Fatalf("DeleteTerminal: %v", err)
	}
	if _, ok := reopened.TerminalByID(first.ID); ok {
		t.Fatal("删除后不应存在")
	}
	if err := reopened.DeleteTerminal(first.ID); err == nil {
		t.Fatal("重复删除应报错")
	}
}

func TestHistoryCleanup(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := st.History()
	id := "s001-42"
	if err := h.Prepare(id); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		h.SessionPath(id):    "dir\n",
		h.BootstrapPath(id):  "# bootstrap\n",
		h.SessionCwdPath(id): `C:\work`,
		h.ScreenPath(id):     "\x1b[1m_ANSI_SCREEN\x1b[0m",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := string(h.ReadSessionScreen(id)); got != "\x1b[1m_ANSI_SCREEN\x1b[0m" {
		t.Fatalf("ReadSessionScreen = %q", got)
	}
	h.Cleanup(id)
	if lines := h.ReadSession(id); len(lines) != 0 {
		t.Fatalf("Cleanup 后命令文件应不存在或为空，got %v", lines)
	}
	for _, pair := range [][2]string{
		{"ps1", h.BootstrapPath(id)},
		{"cwd", h.SessionCwdPath(id)},
	} {
		if _, err := os.Stat(pair[1]); !os.IsNotExist(err) {
			t.Fatalf("%s 文件未清理: %s", pair[0], pair[1])
		}
	}
	h.Cleanup("missing-id") // 不存在时静默
}
