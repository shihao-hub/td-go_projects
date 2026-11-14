package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mail2feishu/internal/feishu"
	"mail2feishu/internal/matcher"
)

type webhookPayload struct {
	MsgType string `json:"msg_type"`
	Card    struct {
		Header struct {
			Title struct {
				Content string `json:"content"`
			} `json:"title"`
			Template string `json:"template"`
		} `json:"header"`
		Body struct {
			Elements []struct {
				Tag     string `json:"tag"`
				Content string `json:"content"`
				Actions []struct {
					URL  string `json:"url"`
					Text struct {
						Content string `json:"content"`
					} `json:"text"`
				} `json:"actions"`
			} `json:"elements"`
		} `json:"body"`
	} `json:"card"`
}

var messageSeq atomic.Int64

func mailBytes(t *testing.T, from, subject, body string, extraHeaders ...string) []byte {
	t.Helper()
	headers := []string{
		"From: " + from,
		"Subject: " + subject,
		fmt.Sprintf("Message-ID: <msg-%d@test>", messageSeq.Add(1)),
		"Content-Type: text/plain; charset=utf-8",
	}
	headers = append(headers, extraHeaders...)
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n"))
}

func githubMail(t *testing.T, subject, body string) []byte {
	return mailBytes(t, "GitHub <notifications@github.com>", subject, body,
		"List-Id: octocat/hello-world <hello-world.octocat.github.com>",
		"X-GitHub-Reason: subscribed",
		"X-GitHub-Sender: someuser",
	)
}

func newTestPipeline(t *testing.T, serverURL string, window, cooldown time.Duration) *Pipeline {
	t.Helper()
	matcherInstance, err := matcher.New([]matcher.Rule{
		{Name: "github", SenderDomains: []string{"github.com"}},
	})
	if err != nil {
		t.Fatalf("matcher.New() 错误: %v", err)
	}
	pipeline, err := New(Options{
		Matcher:        matcherInstance,
		Feishu:         feishu.NewClient(serverURL, "", feishu.WithRetry(1, 0)),
		DebounceWindow: window,
		ThreadCooldown: cooldown,
	})
	if err != nil {
		t.Fatalf("New() 错误: %v", err)
	}
	t.Cleanup(pipeline.Close)
	return pipeline
}

func TestHandleMailDropsNonWhitelisted(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	pipeline := newTestPipeline(t, server.URL, 10*time.Millisecond, 0)
	if err := pipeline.HandleMail(1, mailBytes(t, "boss@example.com", "普通邮件", "hello")); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("非白名单邮件不应推送，实际请求 %d 次", got)
	}
}

func TestHandleMailPushesGitHubCard(t *testing.T) {
	requests := make(chan webhookPayload, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload webhookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("解析请求失败: %v", err)
		}
		requests <- payload
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	pipeline := newTestPipeline(t, server.URL, 10*time.Millisecond, 0)
	raw := githubMail(t, "[octocat/hello-world] New issue: Something is broken (#42)",
		"body\nhttps://github.com/octocat/hello-world/issues/42\n")
	if err := pipeline.HandleMail(7, raw); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}

	select {
	case payload := <-requests:
		if payload.MsgType != "interactive" {
			t.Errorf("MsgType = %q", payload.MsgType)
		}
		if want := "octocat/hello-world · Issue #42"; payload.Card.Header.Title.Content != want {
			t.Errorf("卡片标题 = %q, want %q", payload.Card.Header.Title.Content, want)
		}
		if payload.Card.Header.Template != feishu.TemplateBlue {
			t.Errorf("卡片配色 = %q", payload.Card.Header.Template)
		}
		if len(payload.Card.Body.Elements) != 2 {
			t.Fatalf("元素数量 = %d", len(payload.Card.Body.Elements))
		}
		action := payload.Card.Body.Elements[1]
		if len(action.Actions) != 1 || action.Actions[0].URL != "https://github.com/octocat/hello-world/issues/42" {
			t.Errorf("按钮 = %+v", action.Actions)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待推送超时")
	}
}

func TestHandleMailDebouncesSameThread(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	pipeline := newTestPipeline(t, server.URL, 40*time.Millisecond, 0)
	first := githubMail(t, "[octocat/hello-world] Something is broken (#42)", "first")
	second := githubMail(t, "Re: [octocat/hello-world] Something is broken (#42)", "second")

	if err := pipeline.HandleMail(1, first); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}
	if err := pipeline.HandleMail(2, second); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("同一讨论串窗口内应聚合为 1 次推送，实际 %d 次", got)
	}
}

func TestHandleMailDropsDuringCooldown(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	pipeline := newTestPipeline(t, server.URL, 10*time.Millisecond, 300*time.Millisecond)
	raw := githubMail(t, "[octocat/hello-world] Something is broken (#42)", "first")
	if err := pipeline.HandleMail(1, raw); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("首次推送 = %d 次, want 1", got)
	}

	if err := pipeline.HandleMail(2, raw); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("冷却期内不应再推送，实际 %d 次", got)
	}
}
