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
