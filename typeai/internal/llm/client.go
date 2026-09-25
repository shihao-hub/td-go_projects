// Package llm 实现 OpenAI 兼容 /chat/completions 流式客户端。
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

// Message 是发送给聊天接口的一条消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// DeltaKind 区分思考流和正式回答流。
type DeltaKind string

const (
	DeltaReasoning DeltaKind = "reasoning"
	DeltaAnswer    DeltaKind = "answer"
)

// Delta 是 SSE 返回的一段增量。
type Delta struct {
	Kind DeltaKind
	Text string
}

// Client 只依赖标准库 HTTP 客户端，便于测试和保持启动开销最低。
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type chatChunk struct {
	Choices []struct {
		Delta struct {
			ReasoningContent string `json:"reasoning_content"`
			Content          string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// HTTPError 表示上游返回非 2xx。
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

// ChatStream 发起流式对话，并把每个非空增量交给 onDelta。
func (c *Client) ChatStream(ctx context.Context, messages []Message, onDelta func(delta Delta)) (string, error) {
	if onDelta == nil {
		onDelta = func(Delta) {}
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	body, err := json.Marshal(chatRequest{
		Model:    c.Model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 LLM 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		return "", &HTTPError{Status: resp.StatusCode, Body: string(raw)}
	}

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		payload, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if payload == "[DONE]" {
			break
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.ReasoningContent != "" {
			onDelta(Delta{Kind: DeltaReasoning, Text: delta.ReasoningContent})
		}
		if delta.Content != "" {
			full.WriteString(delta.Content)
			onDelta(Delta{Kind: DeltaAnswer, Text: delta.Content})
		}
	}
	if err := scanner.Err(); err != nil {
		return full.String(), fmt.Errorf("读取流式响应失败: %w", err)
	}
	return full.String(), nil
}
