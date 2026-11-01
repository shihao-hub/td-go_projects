package updater

import (
	"path/filepath"
	"testing"
)

func TestVersionedArchiveCacheDir(t *testing.T) {
	baseDir := filepath.Join("C:", "Users", "test", "AppData", "Local", "Zed", "external_agents", "registry", "antigravity-acp")
	version := "1.4.5"
	archiveURL := "https://dl.google.com/agy-extensions/releases/windows/agy-acp-server-1.4.5-windows-x86_64.zip"
	sha256 := "d24f4c73c9f96d4f44384e281c855a56c72d8eaa9c5b0fad7ccb1635c6b63f50"

	got := VersionedArchiveCacheDir(baseDir, version, archiveURL, sha256)
	wantDirName := "v_1.4.5_6c2906ba8ee98ee2_9db1e24e09a5ce41"
	want := filepath.Join(baseDir, wantDirName)

	if got != want {
		t.Fatalf("VersionedArchiveCacheDir mismatch:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestSanitizePathComponent(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"1.4.5", "1.4.5"},
		{"v1.0.0-beta.1", "v1.0.0-beta.1"},
		{"release/2.3.5", "release-2.3.5"},
		{"release:2.3.5", "release-2.3.5"},
		{"", "unknown"},
		{"!@#$%^&*", "--------"},
	}

	for _, c := range cases {
		got := SanitizePathComponent(c.input)
		if got != c.want {
			t.Errorf("SanitizePathComponent(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
