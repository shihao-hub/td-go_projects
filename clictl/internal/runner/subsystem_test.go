package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsConsoleExe(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"cmd.exe 是命令行程序", filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), true},
		{"notepad.exe 是窗口程序", filepath.Join(os.Getenv("SystemRoot"), "System32", "notepad.exe"), false},
		{"不存在的文件", filepath.Join(t.TempDir(), "missing.exe"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := os.Stat(c.path); err != nil && c.want {
				t.Skipf("系统文件不存在: %s", c.path)
			}
			if got := IsConsoleExe(c.path); got != c.want {
				t.Fatalf("IsConsoleExe(%s) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestIsConsoleExeRejectsNonPE(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.exe")
	if err := os.WriteFile(path, []byte("MZ not a real PE file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsConsoleExe(path) {
		t.Fatal("非 PE 文件应返回 false")
	}
}
