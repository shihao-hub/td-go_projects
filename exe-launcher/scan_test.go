package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanDirExe(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("top.exe")
	mk("sub/nested.EXE")
	mk("dist/DemoApp/DemoApp.exe")
	for _, d := range []string{".git", "node_modules", ".venv", "venv", "env", "__pycache__", ".idea", ".vscode"} {
		mk(d + "/x.exe")
	}
	mk("readme.txt")

	got, err := scanDirExe(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "dist", "DemoApp", "DemoApp.exe"),
		filepath.Join(root, "sub", "nested.EXE"),
		filepath.Join(root, "top.exe"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("扫描结果不符:\n got  %v\n want %v", got, want)
	}
}

func TestIsNoiseDir(t *testing.T) {
	for _, n := range []string{".git", ".GIT", "Node_Modules", ".VENV"} {
		if !isNoiseDir(n) {
			t.Errorf("%q 应为噪音目录", n)
		}
	}
	for _, n := range []string{"dist", "src", "build"} {
		if isNoiseDir(n) {
			t.Errorf("%q 不应视为噪音目录", n)
		}
	}
}

func TestExeBaseName(t *testing.T) {
	cases := map[string]string{
		`C:\a\b\Foo.exe`: "Foo",
		`C:\a\b\Foo.EXE`: "Foo",
		"bar.exe":        "bar",
		`D:\x\a.b.exe`:   "a.b",
	}
	for in, want := range cases {
		if got := exeBaseName(in); got != want {
			t.Errorf("exeBaseName(%q) = %q, want %q", in, got, want)
		}
	}
}
