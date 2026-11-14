package runtime

import (
	"os"
	"path/filepath"
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
