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
	"mail2feishu/internal/outbox"
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

// testUIDValidity 是测试用的固定 UIDVALIDITY，值不影响业务逻辑。
const testUIDValidity = uint32(1)

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
	return newTestPipelineAtDir(t, t.TempDir(), serverURL, window, cooldown, auditor, 0)
}

// newTestPipelineAtDir 在指定目录上创建 Pipeline，用于模拟「进程重启」：
// 两次创建共享同一个 outbox 目录，等价于先后两个进程。
func newTestPipelineAtDir(t *testing.T, dir, serverURL string, window, cooldown time.Duration, auditor Auditor, retryBase time.Duration) *Pipeline {
	t.Helper()
	store, err := outbox.Open(dir)
	if err != nil {
		t.Fatalf("outbox.Open() 错误: %v", err)
	}
	matcherInstance, err := matcher.New([]matcher.Rule{
		{Name: "github", SenderDomains: []string{"github.com"}},
	})
	if err != nil {
		t.Fatalf("matcher.New() 错误: %v", err)
	}
	opts := Options{
		Matcher:        matcherInstance,
		Feishu:         feishu.NewClient(serverURL, "", feishu.WithRetry(1, 0)),
		Outbox:         store,
		DebounceWindow: window,
		ThreadCooldown: cooldown,
		RetryBase:      retryBase,
		Auditor:        auditor,
	}
	pipeline, err := New(opts)
	if err != nil {
		t.Fatalf("New() 错误: %v", err)
	}
	t.Cleanup(pipeline.Close)
	return pipeline
}

func waitAuditEvents(t *testing.T, auditor *fakeAuditor, count int) []audit.Event {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
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
	if err := pipeline.HandleMail(3, testUIDValidity, mailBytes(t, "boss@example.com", "ordinary mail", "hello")); err != nil {
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
	if err := pipeline.HandleMail(9, testUIDValidity, raw); err != nil {
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
	if push.UID != 9 {
		t.Errorf("推送事件应带 UID，got %+v", push)
	}
}

func TestHandleMailAuditsParseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	auditor := &fakeAuditor{}
	pipeline := newTestPipelineWithAuditor(t, server.URL, 10*time.Millisecond, 0, auditor)
	// 解析失败是确定性错误：审计后视为已处理，水位线可以推进，
	// 否则一封坏邮件会永久卡住整个收信链路。
	if err := pipeline.HandleMail(11, testUIDValidity, nil); err != nil {
		t.Fatalf("解析失败不应阻塞水位线推进: %v", err)
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
	if err := pipeline.HandleMail(1, testUIDValidity, mailBytes(t, "boss@example.com", "普通邮件", "hello")); err != nil {
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
	if err := pipeline.HandleMail(7, testUIDValidity, raw); err != nil {
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

	if err := pipeline.HandleMail(1, testUIDValidity, first); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}
	if err := pipeline.HandleMail(2, testUIDValidity, second); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
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

	auditor := &fakeAuditor{}
	pipeline := newTestPipelineWithAuditor(t, server.URL, 10*time.Millisecond, 300*time.Millisecond, auditor)
	raw := githubMail(t, "[octocat/hello-world] Something is broken (#42)", "first")
	if err := pipeline.HandleMail(1, testUIDValidity, raw); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("首次推送 = %d 次, want 1", got)
	}

	if err := pipeline.HandleMail(2, testUIDValidity, raw); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("冷却期内不应再推送，实际 %d 次", got)
	}

	// 冷却丢弃必须能从审计里与「未命中白名单」区分开。
	var found bool
	for _, event := range auditor.snapshot() {
		if event.Action == audit.ActionCooldownDropped && event.UID == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("缺少 cooldown_dropped 审计事件: %+v", auditor.snapshot())
	}
}

// TestPendingMailSurvivesCloseAndRestart 复现并锁定「退出即丢信」：
// 邮件已进入防抖窗口、UID 水位线已推进，随后进程退出；重启后必须补投。
func TestPendingMailSurvivesCloseAndRestart(t *testing.T) {
	requests := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	raw := githubMail(t, "[octocat/hello-world] Broken (#42)", "body")

	first := newTestPipelineAtDir(t, dir, server.URL, time.Hour, 0, nil, 0)
	if err := first.HandleMail(1, testUIDValidity, raw); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}
	first.Close()

	// 模拟进程重启：重新打开同一个 outbox 目录，条目必须仍在等待投递。
	store, err := outbox.Open(dir)
	if err != nil {
		t.Fatalf("outbox.Open() 错误: %v", err)
	}
	if got := store.Count(outbox.StatePending); got != 1 {
		t.Fatalf("重启后应有 1 条待投递记录，实际 %d", got)
	}

	newTestPipelineAtDir(t, dir, server.URL, 5*time.Millisecond, 0, nil, 0)

	select {
	case <-requests:
	case <-time.After(3 * time.Second):
		t.Fatal("重启后未补投待投递邮件（通知丢失）")
	}
}

// TestPushFailureKeepsEntryForRetry 锁定「投递失败后重启仍能补投」：
// 飞书重试耗尽不能让邮件从队列里消失。
func TestPushFailureKeepsEntryForRetry(t *testing.T) {
	var failing atomic.Bool
	failing.Store(true)
	requests := make(chan struct{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		requests <- struct{}{}
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	auditor := &fakeAuditor{}
	raw := githubMail(t, "[octocat/hello-world] Broken (#42)", "body")

	first := newTestPipelineAtDir(t, dir, server.URL, 5*time.Millisecond, 0, auditor, time.Hour)
	if err := first.HandleMail(1, testUIDValidity, raw); err != nil {
		t.Fatalf("HandleMail() 错误: %v", err)
	}
	events := waitAuditEvents(t, auditor, 2)
	if events[1].Action != audit.ActionPushFailed {
		t.Fatalf("期望 push_failed，实际 %+v", events[1])
	}
	first.Close()

	store, err := outbox.Open(dir)
	if err != nil {
		t.Fatalf("outbox.Open() 错误: %v", err)
	}
	if store.Count(outbox.StatePending) != 1 && store.Count(outbox.StateFailed) != 1 {
		t.Fatalf("投递失败的邮件必须留在队列里，实际记录: %+v", store.List())
	}

	failing.Store(false)
	newTestPipelineAtDir(t, dir, server.URL, 5*time.Millisecond, 0, nil, 0)

	select {
	case <-requests:
	case <-time.After(3 * time.Second):
		t.Fatal("重启后未补投失败的邮件")
	}
}
