package runner

import (
	"os"
	"path/filepath"
	"testing"

	"clictl/internal/store"
)

func TestBuildSpec(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	tool := store.Tool{
		ID:            1,
		DefaultTarget: store.TargetExe,
		Exe: &store.ExeTarget{
			Path:   self,
			Status: store.StatusActive,
		},
		Source: &store.SourceTarget{
			Program: self,
			Workdir: work,
			Args:    []string{"run", "demo"},
			Type:    store.SourceTypeConsole,
			Status:  store.StatusActive,
		},
	}

	spec, rerr := BuildSpec(tool, store.TargetExe, []string{"--flag"})
	if rerr != nil {
		t.Fatalf("exe spec 失败: %v", rerr)
	}
	if spec.Program != self || len(spec.Args) != 1 || spec.Dir != "" || spec.Target != store.TargetExe {
		t.Fatalf("exe spec 异常: %+v", spec)
	}

	spec, rerr = BuildSpec(tool, store.TargetSource, []string{"--extra"})
	if rerr != nil {
		t.Fatalf("source spec 失败: %v", rerr)
	}
	if spec.Program != self || spec.Dir != work || !spec.Console || spec.Target != store.TargetSource {
		t.Fatalf("source spec 异常: %+v", spec)
	}
	if len(spec.Args) != 3 || spec.Args[0] != "run" || spec.Args[1] != "demo" || spec.Args[2] != "--extra" {
		t.Fatalf("source args 追加异常: %+v", spec.Args)
	}

	if _, rerr := BuildSpec(tool, "bogus", nil); rerr == nil || rerr.Code != "bad_args" {
		t.Fatalf("非法 target 应报 bad_args: %+v", rerr)
	}

	broken := tool
	broken.Exe = &store.ExeTarget{Path: filepath.Join(t.TempDir(), "missing.exe"), Status: store.StatusInvalid}
	if _, rerr := BuildSpec(broken, store.TargetExe, nil); rerr == nil || rerr.ExitCode != 127 {
		t.Fatalf("失效 exe 应 127: %+v", rerr)
	}

	noSource := tool
	noSource.Source = nil
	if _, rerr := BuildSpec(noSource, store.TargetSource, nil); rerr == nil || rerr.Code != "no_source_target" {
		t.Fatalf("缺源码目标应报错: %+v", rerr)
	}
}

func TestFilterAliveForTool(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dead := deadPID(t)
	pidSelf := os.Getpid()

	tool := store.Tool{
		DefaultTarget: store.TargetExe,
		Path:          self,
		Exe:           &store.ExeTarget{Path: self},
		Source:        &store.SourceTarget{Program: self},
	}
	launches := []store.Launch{
		{ID: 1, PID: &pidSelf, Target: store.TargetExe},
		{ID: 2, PID: &pidSelf, Target: store.TargetSource},
		{ID: 3, PID: &dead, Target: store.TargetExe},
		{ID: 4, PID: &pidSelf}, // 旧记录无 target，回退 tool.Path
	}
	alive := FilterAliveForTool(launches, tool)
	if len(alive) != 3 {
		t.Fatalf("应存活 3 条，实际 %+v", alive)
	}

	tool.Source.Program = filepath.Join(t.TempDir(), "other.exe")
	alive = FilterAliveForTool(launches, tool)
	if len(alive) != 2 {
		t.Fatalf("源码路径不匹配后应存活 2 条，实际 %+v", alive)
	}
}
