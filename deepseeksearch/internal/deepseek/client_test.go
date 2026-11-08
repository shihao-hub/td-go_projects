package deepseek

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

// BaseURL 可被用户指向任意代理，异常服务返回超大响应时必须在读取层截停，
// 而不是把内存吃穿
func TestDoRequestRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		chunk := make([]byte, 1<<20) // 1MB/块，共写 12MB 超过 10MB 上限
		for i := 0; i < 12; i++ {
			_, _ = w.Write(chunk)
		}
	}))
	defer server.Close()

	client := NewClient("sk-test-key-123456", server.URL)
	_, err := client.Search(context.Background(), SearchRequest{Query: "test"})
	if err == nil {
		t.Fatalf("超过 10MB 上限的响应应被拒绝")
	}
	if !strings.Contains(err.Error(), "响应体超过") {
		t.Errorf("预期报出响应体大小上限错误，实际: %v", err)
	}
}
