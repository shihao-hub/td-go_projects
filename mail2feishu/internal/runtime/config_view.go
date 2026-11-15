package runtime

import (
	"fmt"
	"strings"
	"time"

	"mail2feishu/internal/config"
	"mail2feishu/internal/matcher"
)

// ConfigView 是配置的界面视图。password/secret 留空表示沿用当前值，
// 布尔标志仅用于告诉界面"已设置"，不会把凭证回传给前端。
type ConfigView struct {
	IMAP       IMAPView       `json:"imap"`
	Feishu     FeishuView     `json:"feishu"`
	Ratelimit  RatelimitView  `json:"ratelimit"`
	TestSender TestSenderView `json:"test_sender"`
	Whitelist  []matcher.Rule `json:"whitelist"`
}

// IMAPView 是 IMAP 配置的界面视图。
type IMAPView struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	PasswordSet  bool   `json:"password_set"`
	Mailbox      string `json:"mailbox"`
	IdleTimeout  string `json:"idle_timeout"`
	PollInterval string `json:"poll_interval"`
}

// FeishuView 是飞书配置的界面视图。
type FeishuView struct {
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret"`
	SecretSet  bool   `json:"secret_set"`
}

// RatelimitView 是频控配置的界面视图。
type RatelimitView struct {
	DebounceWindow string `json:"debounce_window"`
	ThreadCooldown string `json:"thread_cooldown"`
}

// TestSenderView 是内置测试发信配置的界面视图。
type TestSenderView struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	PasswordSet bool   `json:"password_set"`
	FromName    string `json:"from_name"`
}

// NewConfigView 把配置转换为脱敏视图。
func NewConfigView(cfg *config.Config) ConfigView {
	return ConfigView{
		IMAP: IMAPView{
			Host:         cfg.IMAP.Host,
			Port:         cfg.IMAP.Port,
			Username:     cfg.IMAP.Username,
			PasswordSet:  cfg.IMAP.Password != "",
			Mailbox:      cfg.IMAP.Mailbox,
			IdleTimeout:  cfg.IMAP.IdleTimeout.Std().String(),
			PollInterval: cfg.IMAP.PollInterval.Std().String(),
		},
		Feishu: FeishuView{
			WebhookURL: cfg.Feishu.WebhookURL,
			SecretSet:  cfg.Feishu.Secret != "",
		},
		Ratelimit: RatelimitView{
			DebounceWindow: cfg.Ratelimit.DebounceWindow.Std().String(),
			ThreadCooldown: cfg.Ratelimit.ThreadCooldown.Std().String(),
		},
		TestSender: TestSenderView{
			Host:        cfg.TestSender.Host,
			Port:        cfg.TestSender.Port,
			Username:    cfg.TestSender.Username,
			PasswordSet: cfg.TestSender.Password != "",
			FromName:    cfg.TestSender.FromName,
		},
		Whitelist: append([]matcher.Rule(nil), cfg.Whitelist...),
	}
}

// ToConfig 把界面视图还原为配置对象；空时长回落默认值。
func (v ConfigView) ToConfig() (*config.Config, error) {
	idle, err := parseDurationOr(v.IMAP.IdleTimeout, config.DefaultIdleTimeout)
	if err != nil {
		return nil, fmt.Errorf("imap.idle_timeout 无效: %w", err)
	}
	poll, err := parseDurationOr(v.IMAP.PollInterval, config.DefaultPollInterval)
	if err != nil {
		return nil, fmt.Errorf("imap.poll_interval 无效: %w", err)
	}
	debounce, err := parseDurationOr(v.Ratelimit.DebounceWindow, config.DefaultDebounceWindow)
	if err != nil {
		return nil, fmt.Errorf("ratelimit.debounce_window 无效: %w", err)
	}
	cooldown, err := parseDurationOr(v.Ratelimit.ThreadCooldown, config.DefaultThreadCooldown)
	if err != nil {
		return nil, fmt.Errorf("ratelimit.thread_cooldown 无效: %w", err)
	}

	cfg := &config.Config{
		IMAP: config.IMAPConfig{
			Host:         v.IMAP.Host,
			Port:         v.IMAP.Port,
			Username:     v.IMAP.Username,
			Password:     v.IMAP.Password,
			Mailbox:      v.IMAP.Mailbox,
			IdleTimeout:  config.Duration(idle),
			PollInterval: config.Duration(poll),
		},
		Feishu: config.FeishuConfig{
			WebhookURL: v.Feishu.WebhookURL,
			Secret:     v.Feishu.Secret,
		},
		Ratelimit: config.RatelimitConfig{
			DebounceWindow: config.Duration(debounce),
			ThreadCooldown: config.Duration(cooldown),
		},
		TestSender: config.TestSenderConfig{
			Host:     v.TestSender.Host,
			Port:     v.TestSender.Port,
			Username: v.TestSender.Username,
			Password: v.TestSender.Password,
			FromName: v.TestSender.FromName,
		},
		Whitelist: v.Whitelist,
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

func parseDurationOr(raw string, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, fmt.Errorf("不能为负: %s", raw)
	}
	return parsed, nil
}
