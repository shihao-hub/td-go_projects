package engine

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveExePath(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantOK   bool
		wantBase string
	}{
		{
			name:     "裸 exe 名走 System32",
			raw:      "wscript.exe",
			wantOK:   true,
			wantBase: "wscript.exe",
		},
		{
			name:     "绝对路径",
			raw:      `C:\Windows\System32\cmd.exe`,
			wantOK:   true,
			wantBase: "cmd.exe",
		},
		{
			name:     "环境变量展开",
			raw:      `%windir%\System32\cmd.exe`,
			wantOK:   true,
			wantBase: "cmd.exe",
		},
		{
			name:   "不存在的裸名",
			raw:    "no-such-tool-xyzzy-12345.exe",
			wantOK: false,
		},
		{
			name:   "不存在的绝对路径",
			raw:    `C:\NoSuchDir\nope.exe`,
			wantOK: false,
		},
		{
			name:   "空命令",
			raw:    "   ",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveExePath(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("resolveExePath(%q) ok = %v, want %v (resolved: %q)", tt.raw, ok, tt.wantOK, got)
			}
			if tt.wantBase != "" && !strings.EqualFold(filepath.Base(got), tt.wantBase) {
				t.Errorf("resolveExePath(%q) base = %q, want %q", tt.raw, filepath.Base(got), tt.wantBase)
			}
		})
	}
}

// TestGetFilePublisherBareName 回归：裸 exe 名不应被误判为“文件不存在”。
func TestGetFilePublisherBareName(t *testing.T) {
	pub := GetFilePublisher("wscript.exe")
	t.Logf("wscript.exe publisher: %s", pub)
	if pub == "文件不存在" {
		t.Fatalf("裸 exe 名 wscript.exe 被误判为文件不存在")
	}
}
