package agy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/proxy"
	"language_projects/go_projects/acp-title-proxy/internal/store"
)

type Titler struct {
	cfg      *config.AGYConfig
	injector *proxy.Injector
	store    *store.Store

	mu             sync.Mutex
	zedDBPath      string
	titledSessions map[string]bool
	activeRequests map[string]bool
}

func NewTitler(cfg *config.AGYConfig, inj *proxy.Injector, st *store.Store) *Titler {
	if st == nil {
		var err error
		st, err = store.NewStore("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "[AGY Titler] Warning loading persistent store: %v\n", err)
		}
	}

	return &Titler{
		cfg:            cfg,
		injector:       inj,
		store:          st,
		titledSessions: make(map[string]bool),
		activeRequests: make(map[string]bool),
	}
}

// SetZedDBPath overrides the default Zed database path (mainly for unit tests).
func (t *Titler) SetZedDBPath(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.zedDBPath = path
}

func (t *Titler) getZedDBPath() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.zedDBPath
}

func (t *Titler) OnSessionNew(sessionID string) {
	// Ready to accept first prompt for new session
}

func (t *Titler) OnPrompt(sessionID string, prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || sessionID == "" {
		return
	}

	t.mu.Lock()
	// 1. Fast in-memory check or active in-flight request
	if t.titledSessions[sessionID] || t.activeRequests[sessionID] {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	// 2. Persistent store check (survived across process restarts)
	if t.store != nil && t.store.IsTitled(sessionID) {
		t.mu.Lock()
		t.titledSessions[sessionID] = true
		t.mu.Unlock()
		return
	}

	// 3. Zed SQLite database check (double-check if session already has valid title in Zed)
	if title, found := store.CheckZedDBTitle(sessionID, t.getZedDBPath()); found && title != "" {
		t.mu.Lock()
		t.titledSessions[sessionID] = true
		t.mu.Unlock()
		if t.store != nil {
			_ = t.store.MarkTitled(sessionID, title)
		}
		return
	}

	// 4. Truly a new session requiring initial title generation
	t.mu.Lock()
	if t.titledSessions[sessionID] || t.activeRequests[sessionID] {
		t.mu.Unlock()
		return
	}
	t.activeRequests[sessionID] = true
	t.mu.Unlock()

	go t.generateAndInjectTitle(sessionID, prompt)
}

func (t *Titler) generateAndInjectTitle(sessionID, prompt string) {
	defer func() {
		t.mu.Lock()
		delete(t.activeRequests, sessionID)
		t.mu.Unlock()
	}()

	title, err := t.CallLLMDirect(prompt)
	if err != nil || title == "" {
		return
	}

	if err := t.injector.InjectTitle(sessionID, title); err == nil {
		t.mu.Lock()
		t.titledSessions[sessionID] = true
		t.mu.Unlock()

		if t.store != nil {
			_ = t.store.MarkTitled(sessionID, title)
		}
	}
}

// CallLLMDirect tries configured providers in priority order.
func (t *Titler) CallLLMDirect(prompt string) (string, error) {
	if len(t.cfg.Providers) == 0 {
		return "", fmt.Errorf("no LLM providers configured")
	}

	var lastErr error
	for _, p := range t.cfg.Providers {
		if !p.Enabled || strings.TrimSpace(p.APIKey) == "" {
			continue
		}

		title, err := t.callProvider(&p, prompt)
		if err == nil && title != "" {
			return title, nil
		}

		lastErr = err
		fmt.Fprintf(os.Stderr, "[AGY Titler] Provider %s failed: %v, falling back...\n", p.Name, err)
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("all providers skipped or failed")
}

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

func (t *Titler) callProvider(p *config.ProviderConfig, prompt string) (string, error) {
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

	if len(prompt) > 1000 {
		prompt = prompt[:1000]
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
		MaxTokens:            300,
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
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("x-opencode-session", "ses_title_gen")
	req.Header.Set("User-Agent", "opencode/2.2.1")

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
		return "", fmt.Errorf("empty choices from LLM")
	}

	rawTitle := chatResp.Choices[0].Message.Content
	return sanitizeTitle(rawTitle), nil
}

func sanitizeTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'“”‘’《》【】[]()`)
	s = strings.TrimSpace(s)

	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")

	s = strings.TrimRight(s, "。，！？,.!?；;：:")

	runes := []rune(s)
	if len(runes) > 25 {
		return string(runes[:24]) + "…"
	}
	return s
}
