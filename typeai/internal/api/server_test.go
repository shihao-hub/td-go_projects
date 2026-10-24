package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"typeai/internal/config"
	"typeai/internal/service"
	"typeai/internal/session"
)

func TestSessionMetaAndUpdate(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		BaseURL: "http://localhost",
		Model:   "test-model",
	}
	now := time.Now()
	chat, err := service.NewChat(cfg, dir, now)
	if err != nil {
		t.Fatalf("NewChat() error = %v", err)
	}

	server := NewServer(Options{
		Chat:    chat,
		Config:  cfg,
		DataDir: dir,
		Version: "v0.1.0-test",
		Channel: "test",
	})
	handler := server.Handler()

	// 1. Initial state
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/state status = %d, body = %s", w.Code, w.Body.String())
	}
	var stateResp stateResponse
	if err := json.NewDecoder(w.Body).Decode(&stateResp); err != nil {
		t.Fatalf("decode stateResp error = %v", err)
	}
	sessID := stateResp.Session.ID

	// 2. Update session meta (pin, tags, due_date)
	pinned := true
	tags := []string{"待办", "紧急"}
	dueDate := "2026-10-31"
	metaBody, _ := json.Marshal(sessionMetaRequest{
		ID:      sessID,
		Pinned:  &pinned,
		Tags:    &tags,
		DueDate: &dueDate,
	})

	metaReq := httptest.NewRequest(http.MethodPost, "/api/sessions/meta", bytes.NewReader(metaBody))
	metaReq.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, metaReq)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions/meta status = %d, body = %s", w.Code, w.Body.String())
	}

	var metaResp struct {
		Session sessionView `json:"session"`
	}
	if err := json.NewDecoder(w.Body).Decode(&metaResp); err != nil {
		t.Fatalf("decode metaResp error = %v", err)
	}
	if !metaResp.Session.Pinned {
		t.Errorf("expected session to be pinned")
	}
	if len(metaResp.Session.Tags) != 2 || metaResp.Session.Tags[0] != "待办" {
		t.Errorf("unexpected tags: %v", metaResp.Session.Tags)
	}
	if metaResp.Session.DueDate != "2026-10-31" {
		t.Errorf("unexpected due_date: %q", metaResp.Session.DueDate)
	}

	// 3. Verify via GET /api/state
	req = httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var stateResp2 stateResponse
	_ = json.NewDecoder(w.Body).Decode(&stateResp2)
	if !stateResp2.Session.Pinned || len(stateResp2.Session.Tags) != 2 || stateResp2.Session.DueDate != "2026-10-31" {
		t.Errorf("GET /api/state didn't reflect meta: %+v", stateResp2.Session)
	}

	// 4. Test on a disk session
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

	tags2 := []string{"归档"}
	metaBody2, _ := json.Marshal(sessionMetaRequest{
		ID:     store2.ID(),
		Pinned: &pinned,
		Tags:   &tags2,
	})
	metaReq2 := httptest.NewRequest(http.MethodPost, "/api/sessions/meta", bytes.NewReader(metaBody2))
	metaReq2.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, metaReq2)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions/meta for disk session status = %d", w.Code)
	}

	// 5. Test GET /api/sessions lists disk sessions with pinned first
	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, listReq)
	var listResp struct {
		Items []sessionSummaryView `json:"items"`
	}
	_ = json.NewDecoder(w.Body).Decode(&listResp)
	if len(listResp.Items) != 1 {
		t.Fatalf("expected 1 disk session, got %d", len(listResp.Items))
	}
	if !listResp.Items[0].Pinned || len(listResp.Items[0].Tags) != 1 || listResp.Items[0].Tags[0] != "归档" {
		t.Errorf("list summary mismatch: %+v", listResp.Items[0])
	}

	// 6. Test delete session
	delBody, _ := json.Marshal(sessionDeleteRequest{ID: store2.ID()})
	delReq := httptest.NewRequest(http.MethodPost, "/api/sessions/delete", bytes.NewReader(delBody))
	delReq.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, delReq)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions/delete status = %d, body = %s", w.Code, w.Body.String())
	}

	// Check it's gone
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, listReq)
	_ = json.NewDecoder(w.Body).Decode(&listResp)
	if len(listResp.Items) != 0 {
		t.Errorf("expected 0 sessions after deletion, got %d", len(listResp.Items))
	}
}

func TestSessionRenameAPI(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		BaseURL: "http://localhost",
		Model:   "test-model",
	}
	now := time.Now()
	chat, err := service.NewChat(cfg, dir, now)
	if err != nil {
		t.Fatalf("NewChat() error = %v", err)
	}

	server := NewServer(Options{
		Chat:    chat,
		Config:  cfg,
		DataDir: dir,
		Version: "v0.1.0-test",
		Channel: "test",
	})
	handler := server.Handler()

	// 1. Rename current session via POST /api/sessions/rename
	body, _ := json.Marshal(sessionRenameRequest{
		ID:    "",
		Title: "当前会话新标题",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/rename", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions/rename status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Session sessionView `json:"session"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode resp error = %v", err)
	}
	if resp.Session.Title != "当前会话新标题" {
		t.Errorf("resp.Session.Title = %q, want 当前会话新标题", resp.Session.Title)
	}

	// 2. Verify GET /api/state returns the title
	req = httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var stateResp stateResponse
	_ = json.NewDecoder(w.Body).Decode(&stateResp)
	if stateResp.Session.Title != "当前会话新标题" {
		t.Errorf("stateResp.Session.Title = %q, want 当前会话新标题", stateResp.Session.Title)
	}

	// 3. Create a disk session and rename it
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

	body2, _ := json.Marshal(sessionRenameRequest{
		ID:    store2.ID(),
		Title: "历史会话新标题",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/sessions/rename", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req2)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions/rename disk status = %d, body = %s", w.Code, w.Body.String())
	}

	// 4. Verify GET /api/sessions returns the updated title in items
	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, listReq)
	var listResp struct {
		Items []sessionSummaryView `json:"items"`
	}
	_ = json.NewDecoder(w.Body).Decode(&listResp)
	if len(listResp.Items) != 1 {
		t.Fatalf("expected 1 disk session, got %d", len(listResp.Items))
	}
	if listResp.Items[0].Title != "历史会话新标题" {
		t.Errorf("list item title = %q, want 历史会话新标题", listResp.Items[0].Title)
	}
}

func TestUIPreferences(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		BaseURL: "http://localhost",
		Model:   "test-model",
	}
	now := time.Now()
	chat, err := service.NewChat(cfg, dir, now)
	if err != nil {
		t.Fatalf("NewChat() error = %v", err)
	}

	server := NewServer(Options{
		Chat:    chat,
		Config:  cfg,
		DataDir: dir,
		Version: "v0.1.0-test",
		Channel: "test",
	})
	handler := server.Handler()

	// 1. Initial state preferences default to view_mode=tree
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/state status = %d", w.Code)
	}
	var stateResp stateResponse
	if err := json.NewDecoder(w.Body).Decode(&stateResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if stateResp.Preferences.ViewMode != "tree" {
		t.Errorf("default view_mode = %q, want tree", stateResp.Preferences.ViewMode)
	}

	// 2. POST /api/preferences to update view_mode and collapsed_groups
	postBody, _ := json.Marshal(UIPreferences{
		ViewMode:        "list",
		CollapsedGroups: []string{"tag_todo", "tag_archive"},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/preferences", bytes.NewReader(postBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/preferences status = %d, body = %s", w.Code, w.Body.String())
	}
	var updatedPrefs UIPreferences
	if err := json.NewDecoder(w.Body).Decode(&updatedPrefs); err != nil {
		t.Fatalf("decode updatedPrefs error: %v", err)
	}
	if updatedPrefs.ViewMode != "list" {
		t.Errorf("updated view_mode = %q, want list", updatedPrefs.ViewMode)
	}
	if len(updatedPrefs.CollapsedGroups) != 2 {
		t.Errorf("updated collapsed_groups count = %d, want 2", len(updatedPrefs.CollapsedGroups))
	}

	// 3. GET /api/state again and verify it persisted from disk
	req = httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var stateResp2 stateResponse
	_ = json.NewDecoder(w.Body).Decode(&stateResp2)
	if stateResp2.Preferences.ViewMode != "list" {
		t.Errorf("persisted view_mode = %q, want list", stateResp2.Preferences.ViewMode)
	}
	if len(stateResp2.Preferences.CollapsedGroups) != 2 {
		t.Errorf("persisted collapsed_groups count = %d, want 2", len(stateResp2.Preferences.CollapsedGroups))
	}
}
