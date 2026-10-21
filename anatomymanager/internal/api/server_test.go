package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"anatomymanager/internal/model"
	"anatomymanager/internal/storage"
)

const fileURL = "/api/artworks/demo/files/page.html"

// newTestHandler 用临时库起一个完整 handler，走真实路由而非直调 serveFile，
// 保证「URL 解析 → 取 BLOB → 缓存协商」整条链路都被覆盖。
func newTestHandler(t *testing.T) (http.Handler, *storage.Store) {
	t.Helper()
	store, err := storage.NewStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewServer(store, Meta{Channel: "dev"}).Handler(), store
}

// savePage 写入/覆盖同名 page.html，模拟一次 ingest。
func savePage(t *testing.T, store *storage.Store, body string) {
	t.Helper()
	art := &model.Artwork{ID: "demo", Title: "demo", Category: "figure", Tags: []string{"figure"}}
	if err := store.SaveArtwork(art, map[string][]byte{"page.html": []byte(body)}); err != nil {
		t.Fatalf("save artwork: %v", err)
	}
}

// get 发一次 GET，ifNoneMatch 非空时带上 If-None-Match。
func get(h http.Handler, ifNoneMatch string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, fileURL, nil)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestServeFileETagRevalidation 覆盖缓存协商的三条主线：
// 首次 200 下发 ETag → 回传后命中 304 且无 body → 覆盖归档后旧 ETag 立即失效。
func TestServeFileETagRevalidation(t *testing.T) {
	h, store := newTestHandler(t)
	savePage(t, store, "<h1>v1</h1>")

	first := get(h, "")
	if first.Code != http.StatusOK {
		t.Fatalf("首次请求状态码 = %d, want 200", first.Code)
	}
	if ct := first.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html*", ct)
	}
	if cc := first.Header().Get("Cache-Control"); cc != fileCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", cc, fileCacheControl)
	}
	etag := first.Header().Get("ETag")
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Fatalf("ETag = %q, 应为带引号的强校验值", etag)
	}
	if got := first.Body.String(); got != "<h1>v1</h1>" {
		t.Fatalf("body = %q, want <h1>v1</h1>", got)
	}

	unchanged := get(h, etag)
	if unchanged.Code != http.StatusNotModified {
		t.Fatalf("携带同一 ETag 状态码 = %d, want 304", unchanged.Code)
	}
	if unchanged.Body.Len() != 0 {
		t.Fatalf("304 响应体应为空, 实际 %d 字节", unchanged.Body.Len())
	}

	// 同名文件被重新 ingest 覆盖：缓存必须失效，否则用户看到的还是旧图。
	savePage(t, store, "<h1>v2</h1>")
	stale := get(h, etag)
	if stale.Code != http.StatusOK {
		t.Fatalf("内容更新后旧 ETag 状态码 = %d, want 200", stale.Code)
	}
	if newTag := stale.Header().Get("ETag"); newTag == etag {
		t.Fatalf("内容已变但 ETag 未变: %q", newTag)
	}
	if got := stale.Body.String(); got != "<h1>v2</h1>" {
		t.Fatalf("body = %q, want <h1>v2</h1>", got)
	}
}

func TestEtagMatches(t *testing.T) {
	const etag = `"abc123"`
	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"空头", "", false},
		{"精确命中", etag, true},
		{"弱校验前缀", `W/"abc123"`, true},
		{"多值列表命中第二个", `"other", "abc123"`, true},
		{"多值列表未命中", `"other", "another"`, false},
		{"通配", "*", true},
		{"不同摘要", `"def456"`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := etagMatches(c.header, etag); got != c.want {
				t.Fatalf("etagMatches(%q) = %v, want %v", c.header, got, c.want)
			}
		})
	}
}
