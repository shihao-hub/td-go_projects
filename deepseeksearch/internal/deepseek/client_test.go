package deepseek

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestTestAuthUsesFreeModelsEndpoint(t *testing.T) {
	var modelsCalled atomic.Bool
	var searchCalled atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			modelsCalled.Store(true)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "deepseek-chat"}]}`))
		case "/v1/tools/search":
			searchCalled.Store(true)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"results": []}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient("sk-test-key-123456", server.URL)
	err := client.TestAuth(context.Background())
	if err != nil {
		t.Fatalf("TestAuth 应该成功，但报错: %v", err)
	}

	if !modelsCalled.Load() {
		t.Errorf("预期 TestAuth 调用免费的 /models 端点，但未调用")
	}
	if searchCalled.Load() {
		t.Errorf("严重警告：TestAuth 依然调用了付费的 /v1/tools/search 端点！")
	}
}

func TestDoRequestRetryOn503(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error": {"message": "Service Temporarily Unavailable"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results": []}`))
	}))
	defer server.Close()

	client := NewClient("sk-test-key-123456", server.URL)
	req := SearchRequest{Query: "test"}
	resp, err := client.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("预期 503 重试后成功，但返回错误: %v", err)
	}
	if resp == nil {
		t.Fatalf("预期返回有效响应")
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("预期尝试 2 次，实际尝试了 %d 次", attempts)
	}
}
