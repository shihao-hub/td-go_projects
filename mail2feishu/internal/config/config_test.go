package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const minimalYAML = `
imap:
  username: user@qq.com
  password: auth-code
feishu:
  webhook_url: https://open.feishu.cn/open-apis/bot/v2/hook/token
`

func TestParseAppliesDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimalYAML))
	if err != nil {
		t.Fatalf("Parse() 返回错误: %v", err)
	}
	if cfg.IMAP.Host != DefaultIMAPHost {
		t.Errorf("Host = %q, want %q", cfg.IMAP.Host, DefaultIMAPHost)
	}
	if cfg.IMAP.Port != DefaultIMAPPort {
		t.Errorf("Port = %d, want %d", cfg.IMAP.Port, DefaultIMAPPort)
	}
	if cfg.IMAP.Mailbox != DefaultMailbox {
		t.Errorf("Mailbox = %q, want %q", cfg.IMAP.Mailbox, DefaultMailbox)
	}
	if got := cfg.IMAP.IdleTimeout.Std(); got != DefaultIdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", got, DefaultIdleTimeout)
	}
	if got := cfg.IMAP.PollInterval.Std(); got != DefaultPollInterval {
		t.Errorf("PollInterval = %v, want %v", got, DefaultPollInterval)
	}
	if got := cfg.Ratelimit.DebounceWindow.Std(); got != DefaultDebounceWindow {
		t.Errorf("DebounceWindow = %v, want %v", got, DefaultDebounceWindow)
	}
	if got := cfg.Ratelimit.ThreadCooldown.Std(); got != DefaultThreadCooldown {
		t.Errorf("ThreadCooldown = %v, want %v", got, DefaultThreadCooldown)
	}
	if len(cfg.Whitelist) != 0 {
		t.Errorf("Whitelist 长度 = %d, want 0（零信任默认阻断）", len(cfg.Whitelist))
	}
}

func TestParseDurationStrings(t *testing.T) {
	yaml := minimalYAML + `
ratelimit:
  debounce_window: 30s
  thread_cooldown: 1h
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse() 返回错误: %v", err)
	}
	if got := cfg.Ratelimit.DebounceWindow.Std(); got != 30*time.Second {
		t.Errorf("DebounceWindow = %v, want 30s", got)
	}
	if got := cfg.Ratelimit.ThreadCooldown.Std(); got != time.Hour {
		t.Errorf("ThreadCooldown = %v, want 1h", got)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	yaml := minimalYAML + `
whitelst:
  - sender_domains: ["github.com"]
`
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Fatal("未知字段应报错，避免拼写错误导致白名单静默失效")
	}
}

func TestParseRejectsInvalidDuration(t *testing.T) {
	cases := []string{
		`ratelimit: {debounce_window: "abc"}`,
		`ratelimit: {debounce_window: 15}`,
		`ratelimit: {debounce_window: "-15s"}`,
	}
	for _, extra := range cases {
		if _, err := Parse([]byte(minimalYAML + extra)); err == nil {
			t.Fatalf("非法时间值应报错: %s", extra)
		}
	}
}

func TestValidateRejectsMissingRequiredFields(t *testing.T) {
	cases := map[string]string{
		"缺少用户名": `
imap:
  password: auth-code
feishu:
  webhook_url: https://open.feishu.cn/open-apis/bot/v2/hook/token
`,
		"缺少授权码": `
imap:
  username: user@qq.com
feishu:
  webhook_url: https://open.feishu.cn/open-apis/bot/v2/hook/token
`,
		"缺少 webhook": `
imap:
  username: user@qq.com
  password: auth-code
`,
	}
	for name, yaml := range cases {
		if _, err := Parse([]byte(yaml)); err == nil {
			t.Fatalf("%s 应报错", name)
		}
	}
}

func TestValidateRejectsBadWebhook(t *testing.T) {
	cases := []string{"ftp://example.com/hook", "not-a-url", "https://"}
	for _, webhook := range cases {
		yaml := `
imap:
  username: user@qq.com
  password: auth-code
feishu:
  webhook_url: ` + webhook + "\n"
		if _, err := Parse([]byte(yaml)); err == nil {
			t.Fatalf("webhook %q 应报错", webhook)
		}
	}
}

func TestValidateRejectsBadWhitelistRule(t *testing.T) {
	yaml := minimalYAML + `
whitelist:
  - name: broken
`
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Fatal("空白名单规则应报错")
	}
}

func TestTestSenderOptionalAndValidated(t *testing.T) {
	// 整段未配置合法，且默认值填充为 163 官方地址。
	cfg, err := Parse([]byte(minimalYAML))
	if err != nil {
		t.Fatalf("未配置 test_sender 应合法: %v", err)
	}
	if cfg.TestSender.Host != DefaultTestSenderHost || cfg.TestSender.Port != DefaultTestSenderPort {
		t.Errorf("TestSender 默认值 = %+v", cfg.TestSender)
	}

	full := minimalYAML + `
test_sender:
  username: sender@163.com
  password: auth-code
  from_name: mail2feishu 测试
`
	cfg, err = Parse([]byte(full))
	if err != nil {
		t.Fatalf("完整 test_sender 应合法: %v", err)
	}
	if cfg.TestSender.Username != "sender@163.com" || cfg.TestSender.FromName != "mail2feishu 测试" {
		t.Errorf("TestSender = %+v", cfg.TestSender)
	}

	missingPassword := minimalYAML + `
test_sender:
  username: sender@163.com
`
	if _, err := Parse([]byte(missingPassword)); err == nil {
		t.Fatal("填写 username 却缺少授权码应报错")
	}

	badPort := minimalYAML + `
test_sender:
  username: sender@163.com
  password: auth-code
  port: 70000
`
	if _, err := Parse([]byte(badPort)); err == nil {
		t.Fatal("非法端口应报错")
	}
}

func TestExampleConfigIsValid(t *testing.T) {
	path := filepath.Join("..", "..", "config.yaml.example")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取示例配置失败: %v", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("示例配置必须能通过校验: %v", err)
	}
	if len(cfg.Whitelist) == 0 {
		t.Fatal("示例配置应演示至少一条白名单规则")
	}
	if strings.TrimSpace(cfg.Feishu.WebhookURL) == "" {
		t.Fatal("示例配置应包含 webhook_url 占位符")
	}
}
