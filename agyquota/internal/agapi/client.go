package agapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// DefaultEndpoint 配额接口端点（Google 内部 Cloud Code 接口，来自
// Antigravity language_server 日志；未来若变化可通过重新构建更新）。
const DefaultEndpoint = "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"

// RateLimitError 表示接口返回 429（限流），由调用方决定退避与业务错误映射。
type RateLimitError struct {
	StatusCode    int
	Body          string
	retryAfterSec *int
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("配额接口限流（429）: %s", e.Body)
}

// Client 配额接口客户端：429 按固定退避序列自动重试。
type Client struct {
	http        *http.Client
	Endpoint    string
	RetryDelays []time.Duration
	UserAgent   string
	// Logf 可选进度回调（退避重试提示）；nil 时静默。
	Logf func(format string, args ...any)
}

// NewClient 创建默认客户端：429 退避 15s/45s/90s，单请求超时 30s。
func NewClient() *Client {
	return &Client{
		http:        &http.Client{Timeout: 30 * time.Second},
		Endpoint:    DefaultEndpoint,
		RetryDelays: []time.Duration{15 * time.Second, 45 * time.Second, 90 * time.Second},
		UserAgent:   "agyquota/0.1.0",
	}
}

// FetchAvailableModels 调用 fetchAvailableModels，返回原始响应 JSON。
// 请求体 {"project": project}；遇 429 依 RetryDelays 退避重试。
func (c *Client) FetchAvailableModels(ctx context.Context, accessToken, project string) (json.RawMessage, error) {
	body := []byte(fmt.Sprintf(`{"project":%q}`, project))
	for attempt := 0; ; attempt++ {
		raw, err := c.post(ctx, accessToken, body)
		if err == nil {
			return raw, nil
		}
		var rle *RateLimitError
		if !errors.As(err, &rle) || attempt >= len(c.RetryDelays) {
			return nil, err
		}
		// 简单固定退避（Retry-After 若存在且更大则采用）
		wait := c.RetryDelays[attempt]
		if rle.retryAfter() > wait {
			wait = rle.retryAfter()
		}
		if c.Logf != nil {
			c.Logf("配额接口限流（429），%v 后进行第 %d/%d 次重试…", wait, attempt+1, len(c.RetryDelays))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (c *Client) post(ctx context.Context, accessToken string, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造配额请求: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求配额接口: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		rle := &RateLimitError{StatusCode: resp.StatusCode, Body: snippet(respBody)}
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if sec, err := strconv.Atoi(ra); err == nil {
				rle.retryAfterSec = &sec
			}
		}
		return nil, rle
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("配额接口返回 %d: %s", resp.StatusCode, snippet(respBody))
	}
	return json.RawMessage(respBody), nil
}

func (e *RateLimitError) retryAfter() time.Duration {
	if e.retryAfterSec == nil {
		return 0
	}
	return time.Duration(*e.retryAfterSec) * time.Second
}
