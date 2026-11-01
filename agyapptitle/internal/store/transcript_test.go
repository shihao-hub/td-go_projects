package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractUserPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "transcript.jsonl")

	sample := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-01T06:36:27Z","content":"<USER_REQUEST>\n帮我写一个本地文件哈希去重的脚本\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nThe current local time is: 2026-11-01T14:36:27+08:00.\n</ADDITIONAL_METADATA>"}` + "\n"

	if err := os.WriteFile(logPath, []byte(sample), 0644); err != nil {
		t.Fatalf("write sample failed: %v", err)
	}

	got := ExtractUserPrompt(logPath)
	expected := "帮我写一个本地文件哈希去重的脚本"
	if got != expected {
		t.Errorf("ExtractUserPrompt = %q, want %q", got, expected)
	}
}
