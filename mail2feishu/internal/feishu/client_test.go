package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSendCardSuccess(t *testing.T) {
	var got envelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("解析请求体失败: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"success"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	err := client.SendCard(context.Background(), CardOptions{
		Title:   "新邮件",
		Sender:  "a@b.com",
		Subject: "主题",
		LinkURL: "https://example.com/mail/1",
	})
	if err != nil {
		t.Fatalf("SendCard() 错误: %v", err)
	}
	if got.MsgType != "interactive" {
		t.Errorf("MsgType = %q, want interactive", got.MsgType)
	}
	if got.Card.Schema != "2.0" {
		t.Errorf("Card.Schema = %q, want 2.0", got.Card.Schema)
	}
	if got.Card.Header.Title.Content != "新邮件" {
		t.Errorf("卡片标题 = %q", got.Card.Header.Title.Content)
	}
	if got.Timestamp != "" || got.Sign != "" {
		t.Error("未配置 secret 时不应携带签名字段")
	}
}

func TestSendCardIncludesSignature(t *testing.T) {
	const secret = "test-secret"
	var got envelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("解析请求体失败: %v", err)
		}
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, secret)
	if err := client.SendCard(context.Background(), CardOptions{Title: "t"}); err != nil {
		t.Fatalf("SendCard() 错误: %v", err)
	}
	timestamp, err := strconv.ParseInt(got.Timestamp, 10, 64)
	if err != nil {
		t.Fatalf("timestamp 不是秒级整数: %q", got.Timestamp)
	}
	want, err := Sign(secret, timestamp)
	if err != nil {
		t.Fatalf("Sign() 错误: %v", err)
	}
	if got.Sign != want {
		t.Errorf("sign = %q, want %q", got.Sign, want)
	}
}

func TestSendCardRetriesOnServerError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", WithRetry(3, time.Millisecond))
	if err := client.SendCard(context.Background(), CardOptions{Title: "t"}); err != nil {
		t.Fatalf("SendCard() 错误: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("请求次数 = %d, want 3", got)
	}
}

func TestSendCardDoesNotRetryOnBadRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", WithRetry(3, time.Millisecond))
	err := client.SendCard(context.Background(), CardOptions{Title: "t"})
	if err == nil {
		t.Fatal("4xx 应返回错误")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("错误类型 = %T (%v), want *APIError(400)", err, err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("请求次数 = %d, want 1（4xx 不应重试）", got)
	}
}

func TestSendCardReportsBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":19021,"msg":"sign match fail"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "wrong-secret", WithRetry(1, time.Millisecond))
	err := client.SendCard(context.Background(), CardOptions{Title: "t"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T (%v), want *APIError", err, err)
	}
	if apiErr.Code != 19021 || apiErr.Message != "sign match fail" {
		t.Errorf("APIError = %+v", apiErr)
	}
}

func TestSendCardRetriesOnRateLimitCode(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"code":9499,"msg":"too many requests"}`)
			return
		}
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", WithRetry(2, time.Millisecond))
	if err := client.SendCard(context.Background(), CardOptions{Title: "t"}); err != nil {
		t.Fatalf("SendCard() 错误: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("请求次数 = %d, want 2", got)
	}
}

func TestSendCardHonorsContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := NewClient(server.URL, "", WithRetry(5, time.Second))
	if err := client.SendCard(ctx, CardOptions{Title: "t"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("错误 = %v, want context.Canceled", err)
	}
}

func TestSendCardEmptyURL(t *testing.T) {
	client := NewClient("", "")
	if err := client.SendCard(context.Background(), CardOptions{Title: "t"}); err == nil {
		t.Fatal("空 Webhook 地址应报错")
	}
}

func TestSignVector(t *testing.T) {
	got, err := Sign("test-secret", 1700000000)
	if err != nil {
		t.Fatalf("Sign() 错误: %v", err)
	}
	const want = "mbm4Y4oluIPQ00qlBIhX8vAZ0EKv3nw0LuTb91jPL84="
	if got != want {
		t.Errorf("Sign() = %q, want %q", got, want)
	}
}

func TestSignRequiresSecret(t *testing.T) {
	if _, err := Sign("", 1); err == nil {
		t.Fatal("空 secret 应报错")
	}
}
