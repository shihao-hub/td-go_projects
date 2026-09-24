package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fixture 实测真实响应（2026-09-15，max 套餐：一条 5h token 窗口 + MCP 月度）。
const fixture = `{"code":200,"msg":"操作成功","success":true,"data":{"level":"max","limits":[` +
	`{"type":"TIME_LIMIT","unit":5,"number":1,"usage":4000,"currentValue":232,"remaining":3768,"percentage":5,"nextResetTime":1790560841998,"usageDetails":[{"modelCode":"search-prime","usage":207},{"modelCode":"web-reader","usage":8}]},` +
	`{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":2,"nextResetTime":1789481954483}]}}`

func TestFetchUsage(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, fixture)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "my-token")
	usage, raw, err := c.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
	if gotPath != "/api/monitor/usage/quota/limit" {
		t.Fatalf("路径不符: %s", gotPath)
	}
	if gotAuth != "Bearer my-token" {
		t.Fatalf("鉴权头不符: %s", gotAuth)
	}
	if usage.Level != "max" {
		t.Fatalf("level 不符: %s", usage.Level)
	}
	tokens := usage.TokensLimits()
	if len(tokens) != 1 {
		t.Fatalf("TOKENS_LIMIT 应 1 条: %d", len(tokens))
	}
	if tokens[0].Key() != "TOKENS_LIMIT:3:5" {
		t.Fatalf("key 不符: %s", tokens[0].Key())
	}
	if tokens[0].Percentage == nil || *tokens[0].Percentage != 2 {
		t.Fatalf("percentage 不符: %v", tokens[0].Percentage)
	}
	if !strings.Contains(string(raw), `"TOKENS_LIMIT"`) {
		t.Fatalf("data 原文应原样返回: %s", raw)
	}
}

func TestFetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "upstream down")
	}))
	defer srv.Close()

	_, _, err := NewClient(srv.URL, "t").FetchUsage(context.Background())
	var he *HTTPError
	if err == nil || !errors.As(err, &he) || he.Status != http.StatusBadGateway || !strings.Contains(he.Body, "upstream down") {
		t.Fatalf("应返回 HTTPError: %v", err)
	}
}

func TestFetchAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"code":1006,"msg":"无权访问","success":false,"data":null}`)
	}))
	defer srv.Close()

	_, _, err := NewClient(srv.URL, "t").FetchUsage(context.Background())
	var ae *APIError
	if err == nil || !errors.As(err, &ae) || ae.Code != 1006 {
		t.Fatalf("应返回 APIError: %v", err)
	}
}

func TestContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = io.WriteString(w, fixture)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, err := NewClient(srv.URL, "t").FetchUsage(ctx); err == nil {
		t.Fatalf("ctx 超时应返回错误")
	}
}

func TestNewClientDefaults(t *testing.T) {
	if c := NewClient("", "t"); c.BaseURL != DefaultBaseURL {
		t.Fatalf("空 baseURL 应用默认: %s", c.BaseURL)
	}
	if c := NewClient("https://example.com/", "t"); c.BaseURL != "https://example.com" {
		t.Fatalf("尾部斜杠应去除: %s", c.BaseURL)
	}
}
