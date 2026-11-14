package feishu

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout     = 10 * time.Second
	defaultMaxAttempts = 3
	defaultBackoff     = 500 * time.Millisecond
	maxResponseBytes   = 1 << 20
)

// APIError 表示飞书明确拒绝了一次投递（HTTP 非 2xx 或业务错误码非 0）。
type APIError struct {
	StatusCode int
	Code       int
	Message    string
}

func (e *APIError) Error() string {
	switch {
	case e.Code != 0:
		return fmt.Sprintf("飞书返回错误码 %d: %s", e.Code, e.Message)
	case e.StatusCode != 0:
		return fmt.Sprintf("飞书 Webhook 返回 HTTP %d: %s", e.StatusCode, e.Message)
	default:
		return "飞书 Webhook 投递失败: " + e.Message
	}
}

// Client 是飞书群自定义机器人 Webhook 客户端。
type Client struct {
	webhookURL  string
	secret      string
	httpClient  *http.Client
	maxAttempts int
	backoff     time.Duration
}

// Option 用于覆盖客户端默认值，主要供测试使用。
type Option func(*Client)

// WithHTTPClient 替换底层 HTTP 客户端。
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) { c.httpClient = client }
}

// WithRetry 设置最大尝试次数（含首次）与重试基础退避。
func WithRetry(maxAttempts int, backoff time.Duration) Option {
	return func(c *Client) {
		if maxAttempts > 0 {
			c.maxAttempts = maxAttempts
		}
		if backoff >= 0 {
			c.backoff = backoff
		}
	}
}

// NewClient 创建 Webhook 客户端。secret 为空表示机器人未开启签名校验。
func NewClient(webhookURL, secret string, opts ...Option) *Client {
	c := &Client{
		webhookURL:  strings.TrimSpace(webhookURL),
		secret:      strings.TrimSpace(secret),
		httpClient:  &http.Client{Timeout: defaultTimeout},
		maxAttempts: defaultMaxAttempts,
		backoff:     defaultBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// envelope 是 webhook 请求体；签名字段仅在启用 secret 时填写。
type envelope struct {
	Timestamp string     `json:"timestamp,omitempty"`
	Sign      string     `json:"sign,omitempty"`
	MsgType   string     `json:"msg_type"`
	Card      cardObject `json:"card"`
}

type webhookResponse struct {
	Code          int    `json:"code"`
	Msg           string `json:"msg"`
	StatusCode    int    `json:"StatusCode"`
	StatusMessage string `json:"StatusMessage"`
}

func (r webhookResponse) errorCode() int {
	if r.Code != 0 {
		return r.Code
	}
	return r.StatusCode
}

func (r webhookResponse) errorMessage() string {
	if strings.TrimSpace(r.Msg) != "" {
		return r.Msg
	}
	return r.StatusMessage
}

// SendCard 构建卡片并投递；可重试错误会按退避策略重试。
func (c *Client) SendCard(ctx context.Context, opts CardOptions) error {
	if c.webhookURL == "" {
		return errors.New("飞书 Webhook 地址为空")
	}
	payload := envelope{MsgType: "interactive", Card: BuildCard(opts)}
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		err := c.sendOnce(ctx, payload)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable(err) || attempt == c.maxAttempts {
			break
		}
		wait := c.backoff * time.Duration(1<<(attempt-1))
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func (c *Client) sendOnce(ctx context.Context, payload envelope) error {
	if c.secret != "" {
		timestamp := time.Now().Unix()
		sign, err := Sign(c.secret, timestamp)
		if err != nil {
			return fmt.Errorf("生成飞书签名失败: %w", err)
		}
		payload.Timestamp = strconv.FormatInt(timestamp, 10)
		payload.Sign = sign
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化飞书卡片失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建飞书请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求飞书 Webhook 失败: %w", err)
	}
	defer resp.Body.Close()

	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    strings.TrimSpace(string(data)),
		}
	}
	if readErr != nil {
		return fmt.Errorf("读取飞书响应失败: %w", readErr)
	}

	var result webhookResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("解析飞书响应失败（HTTP %d）: %w", resp.StatusCode, err)
	}
	if result.errorCode() != 0 {
		return &APIError{
			StatusCode: resp.StatusCode,
			Code:       result.errorCode(),
			Message:    result.errorMessage(),
		}
	}
	return nil
}

// retryable 只对网络错误、限流和 5xx 重试；4xx 与业务错误码通常意味着
// 地址或签名配置有误，重试没有意义。
func retryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.Code == 9499 { // 飞书业务限流错误码（请求过于频繁）
			return true
		}
		if apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500 {
			return true
		}
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	return false
}

// Sign 计算飞书自定义机器人签名：
// base64(HMAC-SHA256(key = timestamp + "\n" + secret, data = ""))。
func Sign(secret string, timestamp int64) (string, error) {
	if secret == "" {
		return "", errors.New("签名密钥为空")
	}
	key := strconv.FormatInt(timestamp, 10) + "\n" + secret
	mac := hmac.New(sha256.New, []byte(key))
	if _, err := mac.Write([]byte{}); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}
