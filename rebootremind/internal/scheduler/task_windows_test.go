//go:build windows

package scheduler

import (
	"reflect"
	"testing"
)

func TestCreateArgs(t *testing.T) {
	got := CreateArgs("rebootremind_check", `C:\Program Files\rebootremind.exe`)
	want := []string{
		"/Create",
		"/TN", "rebootremind_check",
		"/SC", "HOURLY",
		"/MO", "1",
		"/TR", `"C:\Program Files\rebootremind.exe" check --notify`,
		"/F",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CreateArgs = %#v, want %#v", got, want)
	}
}

func TestIsUnstablePath(t *testing.T) {
	cases := map[string]bool{
		`C:\Users\me\AppData\Local\Temp\go-build123\b001\exe\rebootremind.exe`:        true,
		`C:\Users\me\language_projects\go_projects\rebootremind\bin\rebootremind.exe`: false,
	}
	for path, want := range cases {
		if got := IsUnstablePath(path); got != want {
			t.Fatalf("IsUnstablePath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestNextRun(t *testing.T) {
	english := "TaskName: rebootremind_check\nNext Run Time: 2026/11/7 15:00:00\nStatus: Ready"
	if got := NextRun(english); got != "2026/11/7 15:00:00" {
		t.Fatalf("NextRun = %q", got)
	}
	chinese := "任务名: rebootremind_check\n下次运行时间: 2026/11/7 15:00:00\n状态: 就绪"
	if got := NextRun(chinese); got != "2026/11/7 15:00:00" {
		t.Fatalf("NextRun = %q", got)
	}
	if got := NextRun("no such field"); got != "" {
		t.Fatalf("NextRun = %q, want empty", got)
	}
}
