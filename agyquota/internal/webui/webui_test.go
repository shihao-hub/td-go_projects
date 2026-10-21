package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agyquota/internal/api"
)

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("APPDATA", t.TempDir())
	return New(nil, nil).Handler()
}

func TestHandlerServesWebUI(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET / Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "agyquota") {
		t.Fatal("GET / 未返回 WebUI 页面")
	}
}

func TestHandlerServesStaticAsset(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/style.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /style.css = %d, want 200", rec.Code)
	}
}

func TestHandlerMeta(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/meta", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/meta = %d, want 200", rec.Code)
	}
	var meta metaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("解析 meta 失败: %v", err)
	}
	if meta.Name != "agyquota" {
		t.Fatalf("meta.Name = %q, want agyquota", meta.Name)
	}
}

func TestHandlerCachedQuotaEmpty(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/cache", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/cache = %d, want 200", rec.Code)
	}
	var env api.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("解析包络失败: %v", err)
	}
	if !env.OK || string(env.Data) != "null" {
		t.Fatalf("空缓存应返回 ok=true data=null，实际 ok=%v data=%s", env.OK, env.Data)
	}
}

func TestHandlerUnknownSource(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/quota?source=bogus", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知 source = %d, want 400", rec.Code)
	}
}
