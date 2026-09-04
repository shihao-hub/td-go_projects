package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sse(w http.ResponseWriter, chunks ...string) {
	flusher := w.(http.Flusher)
	for _, c := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", c)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func TestChatStreamHappyPath(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path wrong: %s", r.URL.Path)
		}
		sse(w,
			`{"choices":[{"delta":{"role":"assistant"}}]}`,
			`{"choices":[{"delta":{"content":"你"}}]}`,
			`{"choices":[{"delta":{"content":"好"}}]}`,
			`{"choices":[{"delta":{"content":"吗"}}]}`,
		)
	}))
	defer srv.Close()

	var deltas []string
	c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Model: "m1"}
	full, err := c.ChatStream(context.Background(), []Message{
		{Role: "system", Content: "S"},
		{Role: "user", Content: "U"},
	}, func(d string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatal(err)
	}
	if full != "你好吗" {
		t.Fatalf("full wrong: %q", full)
	}
	if strings.Join(deltas, "") != "你好吗" {
		t.Fatalf("deltas wrong: %v", deltas)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth wrong: %q", gotAuth)
	}
	for _, want := range []string{`"model":"m1"`, `"stream":true`, `"role":"system"`, `"role":"user"`, `"content":"S"`, `"content":"U"`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("request body missing %s: %s", want, gotBody)
		}
	}
}

func TestChatStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "bad", Model: "m"}
	_, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "x"}}, func(string) {})
	he, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("want HTTPError, got %T %v", err, err)
	}
	if he.Status != 401 || !strings.Contains(he.Error(), "invalid api key") {
		t.Fatalf("HTTPError wrong: %+v", he)
	}
}

func TestChatStreamContextCancel(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"首\"}}]}\n\n")
		flusher.Flush()
		hits.Add(1)
		<-r.Context().Done() // 挂住模拟慢模型
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{BaseURL: srv.URL, APIKey: "k", Model: "m"}
	var full string
	var err error
	done := make(chan struct{})
	go func() {
		full, err = c.ChatStream(ctx, []Message{{Role: "user", Content: "x"}}, func(string) {})
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not unblock ChatStream")
	}
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("want context error, got %v", err)
	}
	if full != "首" {
		t.Fatalf("partial text should be kept: %q", full)
	}
}
