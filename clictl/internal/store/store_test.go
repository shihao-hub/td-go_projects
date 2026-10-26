package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fakeExe(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("MZ fake"), 0o644); err != nil {
		t.Fatalf("创建假 exe 失败: %v", err)
	}
	return p
}

// TestMigrateV1ToV3 旧库（无 pid/target/mode、tools 无 source 列）升级后数据完整。
func TestMigrateV1ToV3(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "old.db")
	exe := fakeExe(t, dir, "old.exe")

	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	oldSchema := `
CREATE TABLE tools (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  path TEXT NOT NULL UNIQUE COLLATE NOCASE,
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','invalid')),
  meta TEXT,
  added_at TEXT NOT NULL
);
CREATE TABLE launches (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tool_id INTEGER NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
  started_at TEXT NOT NULL,
  duration_ms INTEGER,
  exit_code INTEGER
);
CREATE INDEX idx_launches_tool ON launches(tool_id, started_at DESC);
`
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(TimeFmt)
	if _, err := db.Exec(`INSERT INTO tools (name,path,description,status,meta,added_at) VALUES (?,?,?,?,?,?)`,
		"old", exe, "旧工具", "active", `{"group":"dev"}`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO launches (tool_id,started_at,duration_ms,exit_code) VALUES (1,?,?,?)`,
		now, 123, 0); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	st, err := OpenAt(dbPath)
	if err != nil {
		t.Fatalf("迁移打开失败: %v", err)
	}
	defer st.Close()

	tool, err := st.GetTool("old")
	if err != nil {
		t.Fatalf("迁移后 GetTool 失败: %v", err)
	}
	if tool.Kind != KindExe || tool.DefaultTarget != TargetExe || tool.Exe == nil || tool.Exe.Path != exe {
		t.Fatalf("迁移结果异常: %+v", tool)
	}
	if tool.Source != nil {
		t.Fatalf("旧行不应有源码目标: %+v", tool.Source)
	}
	if string(tool.Meta) != `{"group":"dev"}` {
		t.Fatalf("meta 丢失: %s", tool.Meta)
	}

	launches, err := st.RecentLaunches(tool.ID, 10)
	if err != nil || len(launches) != 1 {
		t.Fatalf("launches 迁移异常: %+v, err=%v", launches, err)
	}
	if launches[0].DurationMs == nil || *launches[0].DurationMs != 123 {
		t.Fatalf("launch 数据丢失: %+v", launches[0])
	}

	// 新增 target/mode 写入可用
	id, err := st.InsertLaunch(tool.ID, time.Now(), TargetExe, ModeRun)
	if err != nil {
		t.Fatalf("迁移后 InsertLaunch 失败: %v", err)
	}
	if err := st.FinishLaunch(id, 1, 0); err != nil {
		t.Fatal(err)
	}

	// 外键级联仍然有效
	if _, err := st.RemoveTool("old"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM launches`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("删除工具后 launches 应级联清空，实际 %d", count)
	}
}

func TestAddBothTargetsAndConflict(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenAt(filepath.Join(dir, "clictl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	exe := fakeExe(t, dir, "demo.exe")
	work := filepath.Join(dir, "proj")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	tool, err := st.AddTool(AddSpec{
		Name: "demo",
		Exe:  &ExeSpec{Path: exe},
		Source: &SourceSpec{
			Program: exe,
			Workdir: work,
			Args:    []string{"run", "demo-gui"},
			Type:    SourceTypeGUI,
		},
	})
	if err != nil {
		t.Fatalf("AddTool 双目标失败: %v", err)
	}
	if tool.Kind != KindBoth || tool.DefaultTarget != TargetExe {
		t.Fatalf("双目标默认应为 exe: %+v", tool)
	}
	if tool.Source == nil || tool.Source.Command != "demo.exe run demo-gui" {
		t.Fatalf("源码展示异常: %+v", tool.Source)
	}

	// 同 exe 路径冲突
	if _, err := st.AddTool(AddSpec{Name: "other", Exe: &ExeSpec{Path: exe}}); err == nil {
		t.Fatal("重复 exe 路径应冲突")
	}
	// 同源码三元组冲突
	if _, err := st.AddTool(AddSpec{Name: "other2", Source: &SourceSpec{
		Program: exe, Workdir: work, Args: []string{"run", "demo-gui"}, Type: SourceTypeGUI,
	}}); err == nil {
		t.Fatal("重复源码三元组应冲突")
	}
	// 同程序不同工作目录允许
	work2 := filepath.Join(dir, "proj2")
	if err := os.MkdirAll(work2, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddTool(AddSpec{Name: "other3", Source: &SourceSpec{
		Program: exe, Workdir: work2, Args: []string{"run", "demo-gui"}, Type: SourceTypeGUI,
	}}); err != nil {
		t.Fatalf("不同工作目录应允许: %v", err)
	}
}

func TestAttachAndUpdateTargets(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenAt(filepath.Join(dir, "clictl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	exe := fakeExe(t, dir, "demo.exe")
	work := filepath.Join(dir, "proj")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	// 仅 exe → 附加源码，默认仍 exe
	if _, err := st.AddTool(AddSpec{Name: "demo", Exe: &ExeSpec{Path: exe}}); err != nil {
		t.Fatal(err)
	}
	tool, err := st.AttachSource("demo", SourceSpec{
		Program: exe, Workdir: work, Args: []string{"run"}, Type: SourceTypeGUI,
	})
	if err != nil {
		t.Fatalf("AttachSource 失败: %v", err)
	}
	if tool.DefaultTarget != TargetExe || tool.Source == nil {
		t.Fatalf("附加源码后默认应仍为 exe: %+v", tool)
	}

	// 仅源码 → 附加 exe，默认切 exe
	if _, err := st.AddTool(AddSpec{Name: "src", Source: &SourceSpec{
		Program: exe, Workdir: work, Args: []string{"run2"}, Type: SourceTypeConsole,
	}}); err != nil {
		t.Fatal(err)
	}
	tool, err = st.AttachExe("src", ExeSpec{Path: filepath.Join(dir, "src.exe")})
	if err != nil {
		t.Fatalf("AttachExe 失败: %v（需要先创建文件）", err)
	}
	if tool.DefaultTarget != TargetExe || tool.Exe == nil {
		t.Fatalf("附加 exe 后默认应切 exe: %+v", tool)
	}
}

func TestUpdateRemovesTargetAndSwitchesDefault(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenAt(filepath.Join(dir, "clictl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	exe := fakeExe(t, dir, "demo.exe")
	work := filepath.Join(dir, "proj")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddTool(AddSpec{
		Name: "demo",
		Exe:  &ExeSpec{Path: exe},
		Source: &SourceSpec{
			Program: exe, Workdir: work, Args: []string{"run"}, Type: SourceTypeGUI,
		},
	}); err != nil {
		t.Fatal(err)
	}

	// 删除 exe，默认自动切 source
	tool, err := st.UpdateTool("demo", UpdateSpec{
		Description:   "",
		DefaultTarget: TargetExe,
		Source: &SourceSpec{
			Program: exe, Workdir: work, Args: []string{"run"}, Type: SourceTypeGUI,
		},
	})
	if err != nil {
		t.Fatalf("删除 exe 失败: %v", err)
	}
	if tool.Kind != KindSource || tool.DefaultTarget != TargetSource {
		t.Fatalf("删除 exe 后应切 source: %+v", tool)
	}

	// 两个都删空 → 拒绝
	if _, err := st.UpdateTool("demo", UpdateSpec{DefaultTarget: TargetSource}); err == nil {
		t.Fatal("两个目标都删空应拒绝")
	}
}

func TestStatusAnyTarget(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenAt(filepath.Join(dir, "clictl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	exe := fakeExe(t, dir, "demo.exe")
	runnerExe := fakeExe(t, dir, "runner.exe")
	work := filepath.Join(dir, "proj")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddTool(AddSpec{
		Name: "demo",
		Exe:  &ExeSpec{Path: exe},
		Source: &SourceSpec{
			Program: runnerExe, Workdir: work, Args: []string{"run"}, Type: SourceTypeGUI,
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	tools, err := st.ListTools("")
	if err != nil || len(tools) != 1 {
		t.Fatalf("ListTools 异常: %+v, %v", tools, err)
	}
	if tools[0].Status != StatusActive || tools[0].Exe.Status != StatusInvalid || tools[0].Source.Status != StatusActive {
		t.Fatalf("exe 失效但源码有效时总体应 active: %+v", tools[0])
	}

	if err := os.RemoveAll(work); err != nil {
		t.Fatal(err)
	}
	tools, err = st.ListTools("")
	if err != nil || len(tools) != 1 {
		t.Fatalf("ListTools 异常: %+v, %v", tools, err)
	}
	if tools[0].Status != StatusInvalid || tools[0].Exe.Status != StatusInvalid || tools[0].Source.Status != StatusInvalid {
		t.Fatalf("两个目标都失效时总体应 invalid: %+v", tools[0])
	}
	active, err := st.ListTools(StatusActive)
	if err != nil || len(active) != 0 {
		t.Fatalf("active 过滤异常: %+v, %v", active, err)
	}
	invalid, err := st.ListTools(StatusInvalid)
	if err != nil || len(invalid) != 1 {
		t.Fatalf("invalid 过滤异常: %+v, %v", invalid, err)
	}
}

func TestLaunchTargetMode(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenAt(filepath.Join(dir, "clictl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	exe := fakeExe(t, dir, "demo.exe")
	tool, err := st.AddTool(AddSpec{Name: "demo", Exe: &ExeSpec{Path: exe}})
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.InsertLaunch(tool.ID, time.Now(), TargetExe, ModeHidden)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetLaunchPID(id, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	unfinished, err := st.UnfinishedStarts(tool.ID)
	if err != nil || len(unfinished) != 1 {
		t.Fatalf("UnfinishedStarts 异常: %+v, %v", unfinished, err)
	}
	if unfinished[0].Target != TargetExe || unfinished[0].Mode != ModeHidden {
		t.Fatalf("target/mode 丢失: %+v", unfinished[0])
	}
	if err := st.FinishLaunch(id, 10, 0); err != nil {
		t.Fatal(err)
	}
	unfinished, err = st.UnfinishedStarts(tool.ID)
	if err != nil || len(unfinished) != 0 {
		t.Fatalf("闭环后不应再出现在 UnfinishedStarts: %+v, %v", unfinished, err)
	}
}
