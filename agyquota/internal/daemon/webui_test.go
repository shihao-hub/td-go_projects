package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agyquota/internal/service"
	"agyquota/internal/webui"
)

// TestHandlerServesWebUIOnRoot 验证 daemon 端口根路径托管 WebUI，
// 且 /v1/* JSON API 仍保持独立命名空间。
func TestHandlerServesWebUIOnRoot(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	svc := service.New()
	h := &handler{
		svc: svc,
		web: webui.New(svc, nil).Handler(),
	}

	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "agyquota") {
		t.Fatal("daemon 根路径未返回 WebUI 页面")
	}

	rec = httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /v1/missing = %d, want 404", rec.Code)
	}
}
