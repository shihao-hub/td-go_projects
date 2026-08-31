package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"file-sync/config"
	"file-sync/models"
)

func newTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Server{cfg: cfg}, t.TempDir(), t.TempDir()
}

func TestTargetConflict(t *testing.T) {
	s, src1, dst := newTestServer(t)
	src2 := t.TempDir()

	a := models.NewSyncTask("A", src1, dst, nil)
	if err := s.cfg.AddTask(a); err != nil {
		t.Fatal(err)
	}

	// 完全相同的目标（大小写不同）→ 冲突
	b := models.NewSyncTask("B", src2, strings.ToUpper(dst), nil)
	if err := s.checkTargetConflict(b.ID, b.TargetPath); err == nil {
		t.Error("expected conflict for identical target")
	}

	// 自己更新自己 → 不冲突
	if err := s.checkTargetConflict(a.ID, a.TargetPath); err != nil {
		t.Errorf("own target should not conflict: %v", err)
	}

	// 不同目标 → 不冲突
	if err := s.checkTargetConflict(b.ID, filepath.Join(dst, "..", "other")); err != nil {
		t.Errorf("different target should not conflict: %v", err)
	}
}

func TestCreateTaskConflictAPI(t *testing.T) {
	s, src1, dst := newTestServer(t)
	src2 := t.TempDir()

	a := models.NewSyncTask("A", src1, dst, nil)
	_ = s.cfg.AddTask(a)

	body := `{"name":"B","source_path":` + quoteJSON(src2) + `,"target_path":` + quoteJSON(dst) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	req.SetPathValue("id", "x")
	rec := httptest.NewRecorder()
	s.handleCreateTask(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestPathToName(t *testing.T) {
	cases := map[string]string{
		`C:\Users\shawn.zhang\.cache`: "C-Users-shawn.zhang-.cache",
		`C:\Users\shawn.zhang\xxx`:   "C-Users-shawn.zhang-xxx",
		`D:/a/b/`:                    "D-a-b",
		`\\server\share\x`:           "server-share-x",
		`C:\`:                        "C",
	}
	for in, want := range cases {
		if got := pathToName(in); got != want {
			t.Errorf("pathToName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateTaskAutoName(t *testing.T) {
	s, src, dst := newTestServer(t)

	body := `{"name":"","source_path":` + quoteJSON(src) + `,"target_path":` + quoteJSON(dst) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleCreateTask(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var task models.SyncTask
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if want := pathToName(filepath.Clean(src)); task.Name != want {
		t.Errorf("auto name = %q, want %q", task.Name, want)
	}
}

func quoteJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
