// Package config 负责 mail2feishu 的配置加载、默认值填充与校验。
//
// 配置文件中的未知字段会被拒绝：白名单一旦因拼写错误静默失效，
// 用户会以为自己在收推送，实际全部被阻断。
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"mail2feishu/internal/matcher"
)

// 内置默认值。时间类字段写 0 会被视为未配置并回落到这里。
const (
	DefaultIMAPHost = "imap.qq.com"
	DefaultIMAPPort = 993
	DefaultMailbox  = "INBOX"

	DefaultTestSenderHost = "smtp.163.com"
	DefaultTestSenderPort = 465

	DefaultIdleTimeout    = 25 * time.Minute
	DefaultPollInterval   = 5 * time.Minute
	DefaultDebounceWindow = 15 * time.Second
	DefaultThreadCooldown = 10 * time.Minute
)

// Config 是 mail2feishu 的完整配置。
type Config struct {
	IMAP       IMAPConfig       `yaml:"imap"`
	Feishu     FeishuConfig     `yaml:"feishu"`
	Ratelimit  RatelimitConfig  `yaml:"ratelimit"`
	TestSender TestSenderConfig `yaml:"test_sender"`
	Whitelist  []matcher.Rule   `yaml:"whitelist"`
}

// IMAPConfig 是 QQ 邮箱 IMAP 连接配置。
type IMAPConfig struct {
	Host         string   `yaml:"host"`
	Port         int      `yaml:"port"`
	Username     string   `yaml:"username"`
	Password     string   `yaml:"password"`
	Mailbox      string   `yaml:"mailbox"`
	IdleTimeout  Duration `yaml:"idle_timeout"`
	PollInterval Duration `yaml:"poll_interval"`
}

// FeishuConfig 是飞书自定义机器人配置。
type FeishuConfig struct {
	WebhookURL string `yaml:"webhook_url"`
	Secret     string `yaml:"secret"`
}

// TestSenderConfig 是内置「发送测试邮件」使用的 163 发信账号。
// 整段可选：username 为空表示未启用，加载与保存都不会强制要求。
type TestSenderConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	FromName string `yaml:"from_name"`
}

// RatelimitConfig 控制防抖窗口与讨论串冷却时间。
type RatelimitConfig struct {
	DebounceWindow Duration `yaml:"debounce_window"`
	ThreadCooldown Duration `yaml:"thread_cooldown"`
}

// Duration 让 YAML 支持 Go duration 字符串（如 "25m"、"15s"）。
type Duration time.Duration

// UnmarshalYAML 实现 yaml.Unmarshaler。
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("时间值必须是字符串（如 \"15s\"）: %w", err)
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("无法解析时间值 %q: %w", raw, err)
	}
	if parsed < 0 {
		return fmt.Errorf("时间值不能为负: %q", raw)
	}
	*d = Duration(parsed)
	return nil
}

// Std 返回标准库时长。
func (d Duration) Std() time.Duration { return time.Duration(d) }

// MarshalYAML 让配置写回时保持 "15s" 这样的可读形式。
func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

// Load 从指定路径读取并校验配置。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	return Parse(data)
}

// Parse 解析配置内容并完成默认值填充与校验。
func Parse(data []byte) (*Config, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save 校验后原子写回配置文件，权限收紧到仅当前用户可读写。
func (c *Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("写入临时配置失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	return nil
}

// ApplyDefaults 填充未配置的默认值。
func (c *Config) ApplyDefaults() {
	if c.IMAP.Host == "" {
		c.IMAP.Host = DefaultIMAPHost
	}
	if c.IMAP.Port == 0 {
		c.IMAP.Port = DefaultIMAPPort
	}
	if c.IMAP.Mailbox == "" {
		c.IMAP.Mailbox = DefaultMailbox
	}
	if c.TestSender.Host == "" {
		c.TestSender.Host = DefaultTestSenderHost
	}
	if c.TestSender.Port == 0 {
		c.TestSender.Port = DefaultTestSenderPort
	}
	if c.IMAP.IdleTimeout == 0 {
		c.IMAP.IdleTimeout = Duration(DefaultIdleTimeout)
	}
	if c.IMAP.PollInterval == 0 {
		c.IMAP.PollInterval = Duration(DefaultPollInterval)
	}
	if c.Ratelimit.DebounceWindow == 0 {
		c.Ratelimit.DebounceWindow = Duration(DefaultDebounceWindow)
	}
	if c.Ratelimit.ThreadCooldown == 0 {
		c.Ratelimit.ThreadCooldown = Duration(DefaultThreadCooldown)
	}
}

// Validate 检查配置是否可用。返回的错误信息可安全展示给用户，不包含密码。
func (c *Config) Validate() error {
	if strings.TrimSpace(c.IMAP.Host) == "" {
		return errors.New("imap.host 不能为空")
	}
	if c.IMAP.Port < 1 || c.IMAP.Port > 65535 {
		return fmt.Errorf("imap.port 超出范围: %d", c.IMAP.Port)
	}
	if strings.TrimSpace(c.IMAP.Username) == "" {
		return errors.New("imap.username 不能为空")
	}
	if c.IMAP.Password == "" {
		return errors.New("imap.password 不能为空（QQ 邮箱需使用授权码）")
	}
	if strings.TrimSpace(c.Feishu.WebhookURL) == "" {
		return errors.New("feishu.webhook_url 不能为空")
	}
	if err := validateWebhook(c.Feishu.WebhookURL); err != nil {
		return err
	}
	// test_sender 整段可选；只有填写了 username 才视为启用并做完整校验。
	if strings.TrimSpace(c.TestSender.Username) != "" {
		if strings.TrimSpace(c.TestSender.Host) == "" {
			return errors.New("test_sender.host 不能为空")
		}
		if c.TestSender.Port < 1 || c.TestSender.Port > 65535 {
			return fmt.Errorf("test_sender.port 超出范围: %d", c.TestSender.Port)
		}
		if c.TestSender.Password == "" {
			return errors.New("test_sender.password 不能为空（163 邮箱需使用客户端授权码）")
		}
	}
	if c.IMAP.IdleTimeout.Std() <= 0 {
		return errors.New("imap.idle_timeout 必须大于 0")
	}
	if c.IMAP.PollInterval.Std() <= 0 {
		return errors.New("imap.poll_interval 必须大于 0")
	}
	if c.Ratelimit.DebounceWindow.Std() <= 0 {
		return errors.New("ratelimit.debounce_window 必须大于 0")
	}
	if c.Ratelimit.ThreadCooldown.Std() < 0 {
		return errors.New("ratelimit.thread_cooldown 不能为负")
	}
	for i, rule := range c.Whitelist {
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("whitelist 第 %d 条规则无效: %w", i+1, err)
		}
	}
	return nil
}

func validateWebhook(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("feishu.webhook_url 格式非法: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("feishu.webhook_url 必须是 http(s) 地址")
	}
	if parsed.Host == "" {
		return errors.New("feishu.webhook_url 缺少主机名")
	}
	return nil
}
