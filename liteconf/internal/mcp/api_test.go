package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestEnv 返回指向测试 HTTP server 的 apiClient
// （server 起法见 tools_test.go 的 newTestHTTPServer）。
func newTestEnv(t *testing.T) *apiClient {
	t.Helper()
	return newAPIClient(newTestHTTPServer(t))
}

// wantAPIError 断言 err 为 *apiError 且 code 匹配
func wantAPIError(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want apiError %q, got nil", code)
	}
	ae := asAPIError(err)
	if ae.Code != code {
		t.Fatalf("want apiError code %q, got %q (message: %s)", code, ae.Code, ae.Message)
	}
}

func TestDiscoveryEmpty(t *testing.T) {
	c := newTestEnv(t)
	d, err := c.discovery(context.Background())
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if d.Apps == nil || len(d.Apps) != 0 {
		t.Fatalf("want empty non-nil apps, got %#v", d.Apps)
	}
}

func TestPutGetDiscoveryRoundTrip(t *testing.T) {
	c := newTestEnv(t)
	ctx := context.Background()

	cfg1 := map[string]any{"db": map[string]any{"host": "127.0.0.1", "port": 5432}}
	v, err := c.put(ctx, "app1", "dev", cfg1)
	if err != nil || v != 1 {
		t.Fatalf("put #1: version=%d err=%v", v, err)
	}

	cfg2 := map[string]any{"db": map[string]any{"host": "10.0.0.1", "port": 5432}, "log": map[string]any{"level": "info"}}
	v, err = c.put(ctx, "app1", "dev", cfg2)
	if err != nil || v != 2 {
		t.Fatalf("put #2: version=%d err=%v", v, err)
	}

	raw, version, err := c.get(ctx, "app1", "dev")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if version != 2 {
		t.Fatalf("get: want version 2, got %d", version)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	db, ok := got["db"].(map[string]any)
	if !ok || db["host"] != "10.0.0.1" {
		t.Fatalf("content mismatch: %#v", got)
	}

	d, err := c.discovery(ctx)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if len(d.Apps) != 1 || d.Apps[0].App != "app1" {
		t.Fatalf("discovery apps mismatch: %#v", d.Apps)
	}
	if len(d.Apps[0].Envs) != 1 || d.Apps[0].Envs[0].Env != "dev" || d.Apps[0].Envs[0].Version != 2 {
		t.Fatalf("discovery envs mismatch: %#v", d.Apps[0].Envs)
	}
}

func TestGetNotFound(t *testing.T) {
	c := newTestEnv(t)
	_, _, err := c.get(context.Background(), "nope", "dev")
	wantAPIError(t, err, "not_found")
}

func TestInvalidName(t *testing.T) {
	c := newTestEnv(t)
	ctx := context.Background()

	_, _, err := c.get(ctx, "app1", "bad name!")
	wantAPIError(t, err, "invalid_name")

	_, err = c.put(ctx, "bad/name", "dev", map[string]any{"k": "v"})
	wantAPIError(t, err, "invalid_name")
}

func TestPutBodyTooLarge(t *testing.T) {
	c := newTestEnv(t)
	// server 端 body 上限 4MiB；超限返回 413（包络 code 仍为 invalid_json）
	big := map[string]any{"k": strings.Repeat("a", 4<<20+64)}
	_, err := c.put(context.Background(), "app1", "dev", big)
	wantAPIError(t, err, "invalid_json")
}

func TestServerUnreachable(t *testing.T) {
	// 端口 1（tcpmux）本机几乎必然拒绝连接，立即返回错误不等待超时
	c := newAPIClient("http://127.0.0.1:1")
	_, _, err := c.get(context.Background(), "app1", "dev")
	wantAPIError(t, err, CodeUnreachable)
}

func TestBadEnvelopeResponse(t *testing.T) {
	// 响应不是合法 JSON → internal
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello, not json"))
	}))
	defer srv.Close()
	c := newAPIClient(srv.URL)
	_, _, err := c.get(context.Background(), "app1", "dev")
	wantAPIError(t, err, CodeInternal)
}

func TestErrorEnvelopePassthrough(t *testing.T) {
	// 包络 code != ok → 原样透传（含 server 未定义的新 code）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"boom","message":"exploded"}`))
	}))
	defer srv.Close()
	c := newAPIClient(srv.URL)
	_, err := c.discovery(context.Background())
	wantAPIError(t, err, "boom")
}
