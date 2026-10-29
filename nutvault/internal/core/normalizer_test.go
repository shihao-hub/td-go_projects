package core

import (
	"testing"
	"time"
)

func TestNormalizeDir(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    `C:\Users\29580\AppData\Roaming\Zed`,
			expected: `C-Users-29580-AppData-Roaming-Zed`,
		},
		{
			input:    `C:\Users\29580\AppData\Roaming\Zed\`,
			expected: `C-Users-29580-AppData-Roaming-Zed`,
		},
		{
			input:    `D:\workspace\my project\sub:folder`,
			expected: `D-workspace-my project-sub-folder`,
		},
		{
			input:    `C:\`,
			expected: `C`,
		},
	}

	for _, tt := range tests {
		got := NormalizeDir(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeDir(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFormatSnapshotFilename(t *testing.T) {
	tm := time.Date(2026, 10, 26, 22, 52, 18, 0, time.Local)
	name := "settings.json"
	want := "settings-20261026225218.json"
	got := FormatSnapshotFilename(name, tm)
	if got != want {
		t.Errorf("FormatSnapshotFilename() = %q, want %q", got, want)
	}

	noExt := "hosts"
	wantNoExt := "hosts-20261026225218"
	gotNoExt := FormatSnapshotFilename(noExt, tm)
	if gotNoExt != wantNoExt {
		t.Errorf("FormatSnapshotFilename() = %q, want %q", gotNoExt, wantNoExt)
	}
}

func TestAnalyzePath(t *testing.T) {
	info := AnalyzePath(`C:\Users\29580\AppData\Roaming\Zed\settings.json`)
	if info.MappedDir != "C-Users-29580-AppData-Roaming-Zed" {
		t.Errorf("MappedDir = %q, want %q", info.MappedDir, "C-Users-29580-AppData-Roaming-Zed")
	}
	if info.FileName != "settings.json" {
		t.Errorf("FileName = %q, want %q", info.FileName, "settings.json")
	}
}
