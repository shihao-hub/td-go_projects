package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail2feishu/internal/audit"
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

type fakeAuditor struct {
	mu     sync.Mutex
	events []audit.Event
}

func (f *fakeAuditor) Append(event audit.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	return nil
}

func (f *fakeAuditor) snapshot() []audit.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]audit.Event(nil), f.events...)
}

func newTestPipeline(t *testing.T, serverURL string, window, cooldown time.Duration) *Pipeline {
	t.Helper()
	return newTestPipelineWithAuditor(t, serverURL, window, cooldown, nil)
}

func newTestPipelineWithAuditor(t *testing.T, serverURL string, window, cooldown time.Duration, auditor Auditor) *Pipeline {
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
		Auditor:        auditor,
	})
	if err != nil {
		t.Fatalf("New() 错误: %v", err)
	}
	t.Cleanup(pipeline.Close)
	return pipeline
}

func waitAuditEvents(t *testing.T, auditor *fakeAuditor, count int) []audit.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events := auditor.snapshot()
		if len(events) >= count {
			return events
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待审计事件超时，已有 %d 条", len(auditor.snapshot()))
	return nil
}

func TestHandleMailAuditsDropped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	auditor := &fakeAuditor{}
	pipeline := newTestPipelineWithAuditor(t, server.URL, 10*time.Millisecond, 0, auditor)
	if err := pipeline.HandleMail(3, mailBytes(t, "boss@example.com", "ordinary mail", "hello")); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}

	events := waitAuditEvents(t, auditor, 1)
	event := events[0]
	if event.Type != audit.TypeMail || event.UID != 3 {
		t.Fatalf("事件 = %+v", event)
	}
	if event.Action != audit.ActionDropped {
		t.Errorf("Action = %q, want dropped", event.Action)
	}
	if event.From != "boss@example.com" || event.Subject != "ordinary mail" {
		t.Errorf("元信息不完整: %+v", event)
	}
}

func TestHandleMailAuditsWhitelistedAndPush(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	auditor := &fakeAuditor{}
	pipeline := newTestPipelineWithAuditor(t, server.URL, 10*time.Millisecond, 0, auditor)
	raw := githubMail(t, "[octocat/hello-world] New issue: Broken (#42)", "body")
	if err := pipeline.HandleMail(9, raw); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}

	events := waitAuditEvents(t, auditor, 2)
	if events[0].Action != audit.ActionWhitelisted || events[0].Rule != "github" {
		t.Errorf("白名单事件 = %+v", events[0])
	}
	if events[0].Thread != "octocat/hello-world#42" {
		t.Errorf("Thread = %q", events[0].Thread)
	}
	push := events[1]
	if push.Type != audit.TypePush || push.Action != audit.ActionPushed {
		t.Errorf("推送事件 = %+v", push)
	}
	if push.OK == nil || !*push.OK {
		t.Errorf("推送事件 OK = %v", push.OK)
	}
}

func TestHandleMailAuditsParseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	auditor := &fakeAuditor{}
	pipeline := newTestPipelineWithAuditor(t, server.URL, 10*time.Millisecond, 0, auditor)
	if err := pipeline.HandleMail(11, nil); err == nil {
		t.Fatal("空邮件应返回错误")
	}

	events := waitAuditEvents(t, auditor, 1)
	if events[0].Action != audit.ActionParseFailed || events[0].UID != 11 {
		t.Fatalf("解析失败事件 = %+v", events[0])
	}
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
