package runtime

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// ErrTestSenderMissing 表示内置测试发信账号尚未配置，调用方应提示用户去配置页填写。
var ErrTestSenderMissing = errors.New("请先在配置页填写测试发信账号（163 邮箱与授权码）")

const (
	testMailTimeout   = 30 * time.Second
	testMailBodyWidth = 76
)

// BuildTestMail 构造一封 UTF-8 纯文本测试邮件（CRLF 行尾，正文 base64）。
// now 同时用于 Date 与 Message-ID，保证输出可预测、便于单测。
func BuildTestMail(from, fromName, to string, now time.Time) []byte {
	stamp := now.Format("2006-01-02 15:04:05")
	subject := "mail2feishu 测试邮件 " + stamp
	body := "这是一封 mail2feishu 内置测试邮件，用于验证「QQ 收信 → 白名单 → 飞书推送」链路。\r\n" +
		"\r\n" +
		"发信时间：" + stamp + "\r\n" +
		"收到后可在审计页查看放行与推送结果；若白名单未包含该发件人，邮件会被静默丢弃。\r\n" +
		"此邮件可忽略或删除。\r\n"

	var buf bytes.Buffer
	buf.WriteString("From: " + formatMailAddress(from, fromName) + "\r\n")
	buf.WriteString("To: " + to + "\r\n")
	buf.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n")
	buf.WriteString("Date: " + now.Format(time.RFC1123Z) + "\r\n")
	buf.WriteString("Message-ID: " + testMessageID(from, now) + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: base64\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(wrapBase64(base64.StdEncoding.EncodeToString([]byte(body))))
	return buf.Bytes()
}

// SendTestMail 用配置的 163 邮箱向 IMAP 账号发送一封测试邮件。
// 邮件从外部服务商进入 QQ 邮箱，可触发真实的收信 → 白名单 → 飞书链路。
func (r *Runtime) SendTestMail(ctx context.Context) error {
	r.mu.Lock()
	cfg := r.cfg
	r.mu.Unlock()

	sender := cfg.TestSender
	if strings.TrimSpace(sender.Username) == "" {
		return ErrTestSenderMissing
	}
	if sender.Password == "" {
		return fmt.Errorf("%w：缺少授权码", ErrTestSenderMissing)
	}
	recipient := strings.TrimSpace(cfg.IMAP.Username)
	if recipient == "" {
		return errors.New("请先在配置页填写 IMAP 账号，测试邮件将发往该邮箱")
	}

	conn, err := dialTestSender(ctx, sender.Host, sender.Port)
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, sender.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("初始化 SMTP 会话失败: %w", err)
	}
	defer client.Close()

	if err := client.Auth(smtp.PlainAuth("", sender.Username, sender.Password, sender.Host)); err != nil {
		return fmt.Errorf("SMTP 认证失败（163 需使用客户端授权码）: %w", err)
	}
	if err := client.Mail(sender.Username); err != nil {
		return fmt.Errorf("发件地址被服务器拒绝: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("收件地址 %s 被服务器拒绝: %w", recipient, err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("服务器拒绝接收邮件内容: %w", err)
	}
	payload := BuildTestMail(sender.Username, sender.FromName, recipient, time.Now())
	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("提交邮件失败: %w", err)
	}
	// 正文已被服务器接收；QUIT 失败不应让用户以为没发出去而重复发送。
	_ = client.Quit()
	return nil
}

// dialTestSender 建立 465 端口隐式 TLS 连接，并把 ctx 截止时间应用到连接上。
func dialTestSender(ctx context.Context, host string, port int) (*tls.Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: testMailTimeout}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("连接 SMTP 服务器 %s 失败: %w", addr, err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(testMailTimeout)
	}
	_ = raw.SetDeadline(deadline)
	conn := tls.Client(raw, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("建立 TLS 连接失败: %w", err)
	}
	return conn, nil
}

// formatMailAddress 按 RFC 5322 生成 From 头，显示名使用 RFC 2047 编码。
func formatMailAddress(address, name string) string {
	if strings.TrimSpace(name) == "" {
		return address
	}
	return mime.QEncoding.Encode("UTF-8", name) + " <" + address + ">"
}

// testMessageID 用发信时间与发件域生成唯一标识，无需引入随机源。
func testMessageID(from string, now time.Time) string {
	domain := "mail2feishu.local"
	if at := strings.LastIndex(from, "@"); at >= 0 && at < len(from)-1 {
		domain = from[at+1:]
	}
	return fmt.Sprintf("<m2f-test.%d@%s>", now.UnixNano(), domain)
}

// wrapBase64 按 RFC 2045 把 base64 正文折成 76 列。
func wrapBase64(encoded string) string {
	var buf strings.Builder
	for len(encoded) > testMailBodyWidth {
		buf.WriteString(encoded[:testMailBodyWidth])
		buf.WriteString("\r\n")
		encoded = encoded[testMailBodyWidth:]
	}
	buf.WriteString(encoded)
	buf.WriteString("\r\n")
	return buf.String()
}
