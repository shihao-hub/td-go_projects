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

func TestListSessionsPinnedSortingAndMeta(t *testing.T) {
	dir := t.TempDir()
	t1 := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 25, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	store1, _ := New(dir, t1)
	store2, _ := New(dir, t2)
	store3, _ := New(dir, t3)

	sess1 := Session{
		SchemaVersion: SchemaVersion,
		ID:            store1.ID(),
		Model:         "model-1",
		CreatedAt:     t1,
		UpdatedAt:     t1,
		Pinned:        true,
		Tags:          []string{"待办"},
		DueDate:       "2026-10-31",
		Messages: []Message{
			{Role: "user", Content: "sess1 question", CreatedAt: t1},
			{Role: "assistant", Content: "sess1 answer", CreatedAt: t1},
		},
	}
	sess2 := Session{
		SchemaVersion: SchemaVersion,
		ID:            store2.ID(),
		Model:         "model-2",
		CreatedAt:     t2,
		UpdatedAt:     t2,
		Pinned:        false,
		Tags:          []string{"归档"},
		Messages: []Message{
			{Role: "user", Content: "sess2 question", CreatedAt: t2},
			{Role: "assistant", Content: "sess2 answer", CreatedAt: t2},
		},
	}
	sess3 := Session{
		SchemaVersion: SchemaVersion,
		ID:            store3.ID(),
		Model:         "model-3",
		CreatedAt:     t3,
		UpdatedAt:     t3,
		Pinned:        false,
		Messages: []Message{
			{Role: "user", Content: "sess3 question", CreatedAt: t3},
			{Role: "assistant", Content: "sess3 answer", CreatedAt: t3},
		},
	}

	_ = store1.Save(sess1)
	_ = store2.Save(sess2)
	_ = store3.Save(sess3)

	summaries, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions error: %v", err)
	}
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(summaries))
	}

	// sess1 is pinned, so it MUST be first even though its UpdatedAt is earliest
	if summaries[0].ID != store1.ID() {
		t.Errorf("expected first summary to be pinned session %s, got %s", store1.ID(), summaries[0].ID)
	}
	if !summaries[0].Pinned || len(summaries[0].Tags) != 1 || summaries[0].Tags[0] != "待办" || summaries[0].DueDate != "2026-10-31" {
		t.Errorf("summaries[0] meta mismatch: %+v", summaries[0])
	}

	// Between non-pinned sessions, sess3 has later UpdatedAt than sess2, so sess3 is second
	if summaries[1].ID != store3.ID() {
		t.Errorf("expected second summary to be sess3 (%s), got %s", store3.ID(), summaries[1].ID)
	}
	if summaries[2].ID != store2.ID() {
		t.Errorf("expected third summary to be sess2 (%s), got %s", store2.ID(), summaries[2].ID)
	}

	// Test UpdateSessionMeta
	updated, err := UpdateSessionMeta(dir, store2.ID(), func(s *Session) error {
		s.Pinned = true
		s.DueDate = "2026-11-01"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateSessionMeta error: %v", err)
	}
	if !updated.Pinned || updated.DueDate != "2026-11-01" {
		t.Errorf("updated session mismatch: %+v", updated)
	}

	// After updating sess2 to pinned, both sess1 and sess2 are pinned; sess2 has later UpdatedAt so sess2 is first!
	summariesAfter, _ := ListSessions(dir)
	if summariesAfter[0].ID != store2.ID() {
		t.Errorf("expected sess2 to be first now, got %s", summariesAfter[0].ID)
	}
}
