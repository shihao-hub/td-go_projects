package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveRewritesCompleteSessionToSamePath(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 25, 10, 30, 0, 0, time.UTC)
	store, err := New(dir, now)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	first := Session{
		SchemaVersion: SchemaVersion,
		ID:            store.ID(),
		Model:         "test-model",
		CreatedAt:     now,
		UpdatedAt:     now,
		Messages: []Message{
			{Role: "user", Content: "q1", CreatedAt: now},
			{Role: "assistant", Content: "a1", CreatedAt: now},
		},
	}
	if err := store.Save(first); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	path := store.Path()
	if got := filepath.Base(path); !strings.Contains(got, "20260925-103000-"+store.ID()) {
		t.Errorf("path base = %q, id = %q", got, store.ID())
	}

	second := first
	second.Messages = append(second.Messages,
		Message{Role: "user", Content: "q2", CreatedAt: now},
		Message{Role: "assistant", Content: "a2", CreatedAt: now},
	)
	if err := store.Save(second); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	if store.Path() != path {
		t.Fatalf("path changed: old = %q, new = %q", path, store.Path())
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var loaded Session
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(loaded.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(loaded.Messages))
	}
	want := []string{"q1", "a1", "q2", "a2"}
	for i, content := range want {
		if loaded.Messages[i].Content != content {
			t.Errorf("messages[%d].Content = %q, want %q", i, loaded.Messages[i].Content, content)
		}
	}

	entries, err := filepath.Glob(filepath.Join(dir, "sessions", ".*.tmp-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("temporary files remain: %#v", entries)
	}
}
