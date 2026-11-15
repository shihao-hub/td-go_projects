package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail2feishu/internal/audit"
	"mail2feishu/internal/config"
	"mail2feishu/internal/matcher"
)

func TestConfigViewRedactsSecrets(t *testing.T) {
	cfg := &config.Config{
		IMAP: config.IMAPConfig{
			Host: "imap.qq.com", Port: 993, Username: "u@qq.com",
			Password: "auth-code", Mailbox: "INBOX",
			IdleTimeout: config.Duration(25 * time.Minute), PollInterval: config.Duration(5 * time.Minute),
		},
		Feishu: config.FeishuConfig{WebhookURL: "https://open.feishu.cn/hook", Secret: "sign-secret"},
		Ratelimit: config.RatelimitConfig{
			DebounceWindow: config.Duration(15 * time.Second), ThreadCooldown: config.Duration(10 * time.Minute),
		},
		TestSender: config.TestSenderConfig{
			Host: "smtp.163.com", Port: 465, Username: "sender@163.com", Password: "163-code", FromName: "测试",
		},
		Whitelist: []matcher.Rule{{Name: "github", SenderDomains: []string{"github.com"}}},
	}

	view := NewConfigView(cfg)
	if view.IMAP.Password != "" {
		t.Error("密码不应回传给前端")
	}
	if !view.IMAP.PasswordSet {
		t.Error("PasswordSet 应为 true")
	}
	if view.Feishu.Secret != "" {
		t.Error("签名密钥不应回传给前端")
	}
	if !view.Feishu.SecretSet {
		t.Error("SecretSet 应为 true")
	}
	if view.IMAP.IdleTimeout != "25m0s" {
		t.Errorf("IdleTimeout = %q", view.IMAP.IdleTimeout)
	}
	if view.TestSender.Password != "" {
		t.Error("测试发信授权码不应回传给前端")
	}
	if !view.TestSender.PasswordSet {
		t.Error("TestSender.PasswordSet 应为 true")
	}
	if view.TestSender.Username != "sender@163.com" || view.TestSender.FromName != "测试" {
		t.Errorf("TestSender 可编辑字段异常: %+v", view.TestSender)
	}
	if len(view.Whitelist) != 1 || view.Whitelist[0].Name != "github" {
		t.Errorf("Whitelist = %+v", view.Whitelist)
	}
}

func TestConfigViewToConfigAppliesDefaults(t *testing.T) {
	view := ConfigView{
		IMAP:      IMAPView{Host: "imap.qq.com", Port: 993, Username: "u@qq.com", Password: "p", Mailbox: "INBOX"},
		Feishu:    FeishuView{WebhookURL: "https://open.feishu.cn/hook"},
		Whitelist: []matcher.Rule{{Name: "github", SenderDomains: []string{"github.com"}}},
	}
	cfg, err := view.ToConfig()
	if err != nil {
		t.Fatalf("ToConfig() 错误: %v", err)
	}
	if cfg.Ratelimit.DebounceWindow.Std() != config.DefaultDebounceWindow {
		t.Errorf("DebounceWindow = %v", cfg.Ratelimit.DebounceWindow.Std())
	}
	if cfg.Ratelimit.ThreadCooldown.Std() != config.DefaultThreadCooldown {
		t.Errorf("ThreadCooldown = %v", cfg.Ratelimit.ThreadCooldown.Std())
	}
	if cfg.IMAP.IdleTimeout.Std() != config.DefaultIdleTimeout {
		t.Errorf("IdleTimeout = %v", cfg.IMAP.IdleTimeout.Std())
	}
}

func TestConfigViewToConfigRejectsBadDuration(t *testing.T) {
	view := ConfigView{
		IMAP:      IMAPView{Username: "u", Password: "p", IdleTimeout: "not-a-duration"},
		Feishu:    FeishuView{WebhookURL: "https://open.feishu.cn/hook"},
		Whitelist: []matcher.Rule{{Name: "g", SenderDomains: []string{"github.com"}}},
	}
	if _, err := view.ToConfig(); err == nil {
		t.Fatal("非法时长应报错")
	}
}

func TestRecordsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	logger, err := audit.OpenDefault(dir)
	if err != nil {
		t.Fatalf("OpenDefault() 错误: %v", err)
	}
	for uid := uint32(1); uid <= 3; uid++ {
		if err := logger.Append(audit.Event{Type: audit.TypeMail, UID: uid, Action: audit.ActionDropped}); err != nil {
			t.Fatalf("Append() 错误: %v", err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() 错误: %v", err)
	}

	runtime := &Runtime{opts: Options{DataDir: dir}}
	events, err := runtime.Records(10)
	if err != nil {
		t.Fatalf("Records() 错误: %v", err)
	}
	if len(events) != 3 || events[0].UID != 3 || events[2].UID != 1 {
		t.Fatalf("应最新在前，got %+v", events)
	}
}

func TestLogTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mail2feishu.log")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("写入日志失败: %v", err)
	}

	runtime := &Runtime{opts: Options{DataDir: dir}}
	lines, err := runtime.LogTail(2)
	if err != nil {
		t.Fatalf("LogTail() 错误: %v", err)
	}
	if len(lines) != 2 || lines[0] != "b" || lines[1] != "c" {
		t.Fatalf("LogTail() = %v", lines)
	}
}

func TestLogTailMissingFile(t *testing.T) {
	runtime := &Runtime{opts: Options{DataDir: t.TempDir()}}
	lines, err := runtime.LogTail(10)
	if err != nil {
		t.Fatalf("文件不存在不应报错: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("文件不存在应返回空，got %v", lines)
	}
}

func TestBuildTestMail(t *testing.T) {
	now := time.Date(2026, 11, 15, 10, 30, 0, 0, time.FixedZone("CST", 8*3600))
	raw := BuildTestMail("sender@163.com", "mail2feishu 测试", "owner@qq.com", now)

	if !bytes.HasSuffix(raw, []byte("\r\n")) {
		t.Error("邮件应以 CRLF 结尾")
	}
	if strings.Contains(strings.ReplaceAll(string(raw), "\r\n", ""), "\n") {
		t.Error("邮件必须使用 CRLF 行尾")
	}

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("构造的邮件无法解析: %v", err)
	}

	from, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil {
		t.Fatalf("From 头非法: %v", err)
	}
	if from.Address != "sender@163.com" || from.Name != "mail2feishu 测试" {
		t.Errorf("From = %+v", from)
	}
	if got := msg.Header.Get("To"); got != "owner@qq.com" {
		t.Errorf("To = %q", got)
	}

	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("Subject 解码失败: %v", err)
	}
	if !strings.Contains(subject, "测试邮件") || !strings.Contains(subject, "2026-11-15 10:30:00") {
		t.Errorf("Subject = %q", subject)
	}

	date, err := msg.Header.Date()
	if err != nil || !date.Equal(now) {
		t.Errorf("Date = %v（err=%v），want %v", date, err, now)
	}
	if id := msg.Header.Get("Message-ID"); !strings.HasPrefix(id, "<m2f-test.") || !strings.HasSuffix(id, "@163.com>") {
		t.Errorf("Message-ID = %q", id)
	}
	if ct := msg.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") || !strings.Contains(ct, "UTF-8") {
		t.Errorf("Content-Type = %q", ct)
	}

	decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
	if err != nil {
		t.Fatalf("正文 base64 解码失败: %v", err)
	}
	if !strings.Contains(string(decoded), "QQ 收信") || !strings.Contains(string(decoded), "2026-11-15 10:30:00") {
		t.Errorf("正文 = %q", decoded)
	}

	plainMsg, err := mail.ReadMessage(bytes.NewReader(BuildTestMail("sender@163.com", "", "owner@qq.com", now)))
	if err != nil {
		t.Fatalf("空显示名邮件无法解析: %v", err)
	}
	plainFrom, err := mail.ParseAddress(plainMsg.Header.Get("From"))
	if err != nil || plainFrom.Name != "" || plainFrom.Address != "sender@163.com" {
		t.Errorf("空显示名 From = %+v（err=%v）", plainFrom, err)
	}
}

func TestSendTestMailWithoutConfig(t *testing.T) {
	rt := &Runtime{cfg: &config.Config{}}
	err := rt.SendTestMail(context.Background())
	if !errors.Is(err, ErrTestSenderMissing) {
		t.Fatalf("未配置测试发信应返回 ErrTestSenderMissing，got %v", err)
	}
}
