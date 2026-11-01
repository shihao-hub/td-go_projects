package titler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"language_projects/go_projects/agyapptitle/internal/config"
)

type chatRequest struct {
	Model                string        `json:"model"`
	Messages             []chatMessage `json:"messages"`
	Temperature          float64       `json:"temperature"`
	MaxTokens            int           `json:"max_tokens"`
	ReasoningEffort      string        `json:"reasoning_effort,omitempty"`
	ReasoningEffortCamel string        `json:"reasoningEffort,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// GenerateTitle 调用配置的模型链为指定 Prompt 或旧标题生成 4~12 个精炼汉字标题。
func GenerateTitle(cfg *config.Config, prompt, oldTitle string) (string, error) {
	if len(cfg.Providers) == 0 {
		return "", fmt.Errorf("no LLM providers configured in config.json")
	}

	content := strings.TrimSpace(prompt)
	if content == "" || strings.Contains(content, "请提供") {
		content = strings.TrimSpace(oldTitle)
	}
	if content == "" || strings.Contains(content, "请提供") || strings.EqualFold(content, "New Conversation") || strings.EqualFold(content, "New Chat") {
		return "", fmt.Errorf("empty or invalid prompt/title for title generation")
	}

	var lastErr error
	for _, p := range cfg.Providers {
		if !p.Enabled || strings.TrimSpace(p.APIKey) == "" {
			continue
		}

		title, err := callProvider(&p, content)
		if err == nil && title != "" {
			return title, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return "", fmt.Errorf("all providers failed, last error: %w", lastErr)
	}
	return "", fmt.Errorf("no active or valid provider available")
}

func callProvider(p *config.ProviderConfig, prompt string) (string, error) {
	timeout := time.Duration(p.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 4000 * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	endpoint := strings.TrimRight(p.BaseURL, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint += "/chat/completions"
	}

	if len(prompt) > 800 {
		prompt = prompt[:800]
	}

	reqBody := chatRequest{
		Model: p.Model,
		Messages: []chatMessage{
			{
				Role:    "system",
				Content: "你是一个会话标题生成助手。请用 4 到 12 个汉字简明扼要地概括用户的首条提问意图，作为会话标题。仅输出标题文字本身，严禁包含任何标点符号、引号、解释或多余字样。",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature:          0.3,
		MaxTokens:            120,
		ReasoningEffort:      p.ReasoningEffort,
		ReasoningEffortCamel: p.ReasoningEffort,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("User-Agent", "agyapptitle/1.0.0")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", err
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("empty choices from provider %s", p.Name)
	}

	raw := chatResp.Choices[0].Message.Content
	title := SanitizeTitle(raw)
	if strings.Contains(title, "请提供") || strings.Contains(title, "无法生成") || strings.Contains(title, "未提供") || strings.Contains(title, "抱歉") {
		return "", fmt.Errorf("LLM returned non-title message: %s", title)
	}

	return title, nil
}

// SanitizeTitle 清洗模型返回的标题字符串。
func SanitizeTitle(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")

	const cutset = `"'“”‘’《》【】[]()（） 。，！？,.!?；;：:-—_`
	for {
		trimmed := strings.Trim(s, cutset)
		if trimmed == s {
			break
		}
		s = trimmed
	}

	runes := []rune(s)
	if len(runes) > 16 {
		return string(runes[:16]) + "…"
	}
	return s
}
