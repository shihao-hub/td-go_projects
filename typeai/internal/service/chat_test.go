package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"typeai/internal/config"
	"typeai/internal/llm"
	"typeai/internal/session"
)

func TestSendPersistsOnlySuccessfulTurns(t *testing.T) {
	var mu sync.Mutex
	var histories [][]llm.Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		histories = append(histories, request.Messages)
		count := len(histories)
		mu.Unlock()

		if count == 1 {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer-%d\"}}]}\n\n", count)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	dir := t.TempDir()
	chat, err := NewChat(config.Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "test-model",
	}, dir, time.Now())
	if err != nil {
		t.Fatalf("NewChat() error = %v", err)
	}

	var output strings.Builder
	if err := chat.Send(t.Context(), "failed", func(delta llm.Delta) { output.WriteString(delta.Text) }); err == nil {
		t.Fatal("first Send() error = nil, want error")
	}
	sessionDir := filepath.Join(dir, "sessions")
	entries, err := filepath.Glob(filepath.Join(sessionDir, "*.json"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed turn wrote session files: %#v", entries)
	}

	if err := chat.Send(t.Context(), "first", func(delta llm.Delta) { output.WriteString(delta.Text) }); err != nil {
		t.Fatalf("second Send() error = %v", err)
	}
	if err := chat.Send(t.Context(), "second", nil); err != nil {
		t.Fatalf("third Send() error = %v", err)
	}
	if got := output.String(); got != "answer-2" {
		t.Errorf("stream output = %q, want answer-2", got)
	}

	entries, err = filepath.Glob(filepath.Join(sessionDir, "*.json"))
	if err != nil {
		t.Fatalf("Glob() after success error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("session files = %#v, want one", entries)
	}
	raw, err := os.ReadFile(entries[0])
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(raw), "test-key") {
		t.Error("session JSON contains API key")
	}
	var saved session.Session
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := []string{"first", "answer-2", "second", "answer-3"}
	if len(saved.Messages) != len(want) {
		t.Fatalf("messages = %#v, want %#v", saved.Messages, want)
	}
	for i, content := range want {
		if saved.Messages[i].Content != content {
			t.Errorf("messages[%d].Content = %q, want %q", i, saved.Messages[i].Content, content)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(histories) != 3 {
		t.Fatalf("request count = %d, want 3", len(histories))
	}
	if len(histories[0]) != 1 || string(histories[0][0].Content) != `"failed"` {
		t.Errorf("first request messages = %#v", histories[0])
	}
	if len(histories[2]) != 3 || string(histories[2][2].Content) != `"second"` {
		t.Errorf("third request messages = %#v, want prior successful turns plus current input", histories[2])
	}
}

func TestSendRequiresAPIKey(t *testing.T) {
	chat, err := NewChat(config.Config{BaseURL: "http://localhost", Model: "m"}, t.TempDir(), time.Now())
	if err != nil {
		t.Fatalf("NewChat() error = %v", err)
	}
	err = chat.Send(t.Context(), "hello", nil)
	var operation *OperationError
	if !errors.As(err, &operation) {
		t.Fatalf("Send() error = %#v, want OperationError", err)
	}
	if operation.Code != "not_configured" {
		t.Errorf("error code = %q, want not_configured", operation.Code)
	}
}

func TestResumeAlignsModelWithClient(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 9, 22, 0, 0, 0, time.UTC)
	oldStore, err := session.New(dir, now)
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}
	oldSess := session.Session{
		SchemaVersion: session.SchemaVersion,
		ID: oldStore.ID(),
		Model: "glm-5.3-flash",
		CreatedAt: now,
		UpdatedAt: now,
		Branches: []session.Branch{
			{
				ID: session.DefaultRootBranchID,
				CreatedAt: now,
				Messages: []session.Message{
					{Role: "user", Content: "hello", CreatedAt: now},
					{Role: "assistant", Content: "hi", CreatedAt: now},
				},
			},
		},
	}
	if err := oldStore.Save(oldSess); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	chat, err := NewChat(config.Config{
		BaseURL: "http://localhost",
		APIKey: "k",
		Model: "deepseek-v4.1-flash",
	}, dir, time.Now())
	if err != nil {
		t.Fatalf("NewChat() error = %v", err)
	}

	resumed, err := chat.Resume(oldStore.ID())
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Model != "deepseek-v4.1-flash" {
		t.Errorf("resumed.Model = %q, want deepseek-v4.1-flash", resumed.Model)
	}
	if chat.Model() != "deepseek-v4.1-flash" {
		t.Errorf("chat.Model() = %q, want deepseek-v4.1-flash", chat.Model())
	}
	if chat.Session().Model != "deepseek-v4.1-flash" {
		t.Errorf("chat.Session().Model = %q, want deepseek-v4.1-flash", chat.Session().Model)
	}

	all, err := chat.AllSessionSummaries()
	if err != nil {
		t.Fatalf("AllSessionSummaries() error = %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("AllSessionSummaries len = %d, want 1", len(all))
	}
}

func TestChatUpdateSessionMetaAndDelete(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	chat, err := NewChat(config.Config{
		BaseURL: "http://localhost",
		APIKey:  "k",
		Model:   "m",
	}, dir, now)
	if err != nil {
		t.Fatalf("NewChat() error = %v", err)
	}

	pinned := true
	tags := []string{"待办", " 工作 "}
	dueDate := "2026-10-31"

	// Update current in-memory session meta
	updated, err := chat.UpdateSessionMeta(chat.Session().ID, &pinned, &tags, &dueDate)
	if err != nil {
		t.Fatalf("UpdateSessionMeta() error = %v", err)
	}
	if !updated.Pinned || len(updated.Tags) != 2 || updated.Tags[1] != "工作" || updated.DueDate != "2026-10-31" {
		t.Errorf("updated meta mismatch: %+v", updated)
	}

	// Create a disk session
	store2, _ := session.New(dir, now.Add(-time.Hour))
	sess2 := session.Session{
		SchemaVersion: session.SchemaVersion,
		ID:            store2.ID(),
		Model:         "m2",
		CreatedAt:     now.Add(-time.Hour),
		UpdatedAt:     now.Add(-time.Hour),
		Branches: []session.Branch{
			{
				ID:        session.DefaultRootBranchID,
				CreatedAt: now.Add(-time.Hour),
				Messages: []session.Message{
					{Role: "user", Content: "q", CreatedAt: now.Add(-time.Hour)},
					{Role: "assistant", Content: "a", CreatedAt: now.Add(-time.Hour)},
				},
			},
		},
	}
	_ = store2.Save(sess2)

	// Update sess2 via chat.UpdateSessionMeta
	tags2 := []string{"归档"}
	updated2, err := chat.UpdateSessionMeta(store2.ID(), &pinned, &tags2, nil)
	if err != nil {
		t.Fatalf("UpdateSessionMeta for disk session error: %v", err)
	}
	if !updated2.Pinned || len(updated2.Tags) != 1 || updated2.Tags[0] != "归档" {
		t.Errorf("updated2 mismatch: %+v", updated2)
	}

	// Delete sess2
	if err := chat.DeleteSession(store2.ID()); err != nil {
		t.Fatalf("DeleteSession error: %v", err)
	}
	all, _ := chat.AllSessionSummaries()
	if len(all) != 0 {
		t.Errorf("expected 0 disk sessions after deletion, got %d", len(all))
	}
}
