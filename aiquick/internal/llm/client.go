// Package llm 实现 OpenAI 兼容 /chat/completions 流式客户端：
// aiquickd 用它调用 LLM（默认智谱 GLM），把 SSE 增量转交给回调。
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Message 一条对话消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client OpenAI 兼容客户端。零值可用前需设置三个字段。
type Client struct {
	BaseURL string // 例如 https://open.bigmodel.cn/api/paas/v4
	APIKey  string
	Model   string
	HTTP    *http.Client // nil 时用 http.DefaultClient
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type chatChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// HTTPError 上游返回非 2xx 时携带状态码与响应体片段。
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	body := e.Body
	if len(body) > 300 {
		body = body[:300] + "..."
	}
	return fmt.Sprintf("LLM HTTP %d: %s", e.Status, body)
}

// ChatStream 发起流式对话。onDelta 对每个非空增量调用（可能多次），
// 返回拼接后的完整文本。ctx 取消时返回包装后的 context 错误。
func (c *Client) ChatStream(ctx context.Context, msgs []Message, onDelta func(delta string)) (string, error) {
	if c.HTTP == nil {
		c.HTTP = http.DefaultClient
	}
	body, err := json.Marshal(chatRequest{Model: c.Model, Messages: msgs, Stream: true})
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("request LLM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return "", &HTTPError{Status: resp.StatusCode, Body: string(raw)}
	}

	var full strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue // SSE 注释/空行
		}
		payload, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue // 忽略 event:/id:/retry: 等字段
		}
		payload = strings.TrimSpace(payload)
		if payload == "[DONE]" {
			break
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // 容忍个别不合规行
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		if d := chunk.Choices[0].Delta.Content; d != "" {
			full.WriteString(d)
			onDelta(d)
		}
	}
	if err := sc.Err(); err != nil {
		return full.String(), fmt.Errorf("read stream: %w", err)
	}
	return full.String(), nil
}
