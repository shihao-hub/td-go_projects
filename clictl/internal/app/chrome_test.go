package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocateChromeOverride(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "chrome.exe")
	if err := os.WriteFile(exe, []byte("stub"), 0o644); err != nil {
		t.Fatalf("准备测试文件失败: %v", err)
	}

	got, err := LocateChrome(exe)
	if err != nil {
		t.Fatalf("显式覆盖应直接命中: %v", err)
	}
	if got != exe {
		t.Fatalf("期望 %q，实际 %q", exe, got)
	}
}

// 显式覆盖指向不存在的文件时必须报错，不能静默回退到别处的 chrome。
func TestLocateChromeOverrideMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nonexistent", "chrome.exe")
	if _, err := LocateChrome(missing); err == nil {
		t.Fatal("覆盖路径不存在时应返回错误")
	}
}

func TestChromeProfileDirPerChannel(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LocalAppData", base)

	dir, err := ChromeProfileDir()
	if err != nil {
		t.Fatalf("计算 profile 目录失败: %v", err)
	}
	want := filepath.Join(base, "language_projects", chromeAppsRoot, "dev")
	if dir != want {
		t.Fatalf("期望 %q，实际 %q", want, dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("profile 目录未按需创建: %v", err)
	}
	if !strings.Contains(dir, "chrome-apps") {
		t.Fatalf("共享 profile 路径缺少 chrome-apps 片段: %q", dir)
	}
}

func TestWantsChromeWindow(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"clictl-gui.exe"}, false},
		{[]string{"clictl-gui.exe", "--hidden"}, false},
		{[]string{"clictl-gui.exe", "--chrome"}, true},
		{[]string{"clictl-gui.exe", "--chrome", "--hidden"}, false},
		{[]string{"clictl-gui.exe", "--tray", "--chrome"}, false},
	}
	for _, c := range cases {
		if got := wantsChromeWindow(c.args); got != c.want {
			t.Fatalf("wantsChromeWindow(%v) = %v，期望 %v", c.args, got, c.want)
		}
	}
}
