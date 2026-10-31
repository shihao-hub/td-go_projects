package agy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/store"
)

// maxTitleAttempts 限制单个会话在同一进程内的标题生成尝试次数，
// 防止模型链持续失败时每条消息都消耗一次 API 配额。
const maxTitleAttempts = 3

// Injector 是标题注入目标。proxy.Injector（stdio 直连）与 mux.Daemon（共享后端）
// 都实现该接口，从而让标题能力同时适用于两种运行模式。
type Injector interface {
	InjectTitle(sessionID, title string) error
}

type Titler struct {
	cfg      *config.AGYConfig
	injector Injector
	store    *store.Store

	mu             sync.Mutex
	zedDBPath      string
	titledSessions map[string]bool
	activeRequests map[string]bool
	attempts       map[string]int
}

func NewTitler(cfg *config.AGYConfig, inj Injector, st *store.Store) *Titler {
	if st == nil {
		var err error
		st, err = store.NewStore("")
		if err != nil {
			log.Printf("[AGY Titler] 加载持久化标题存储失败: %v", err)
		}
	}

	return &Titler{
		cfg:            cfg,
		injector:       inj,
		store:          st,
		titledSessions: make(map[string]bool),
		activeRequests: make(map[string]bool),
		attempts:       make(map[string]int),
	}
}

// SetZedDBPath 覆盖默认的 Zed 数据库路径（主要用于单元测试）。
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
	// 就绪，等待新会话的首个 prompt
}

func (t *Titler) OnPrompt(sessionID string, prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || sessionID == "" {
		return
	}

	// 1. 内存快路径：已命名、请求处理中或已耗尽尝试次数
	t.mu.Lock()
	if t.titledSessions[sessionID] || t.activeRequests[sessionID] || t.attempts[sessionID] >= maxTitleAttempts {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	// 2. 持久化存储：本代理此前已成功注入过标题
	if t.store != nil && t.store.IsTitled(sessionID) {
		t.markTitled(sessionID, "")
		return
	}

	// 3. 参考 Zed 的会话记录：
	//    - title_override 非空 → 用户手动命名，尊重并跳过；
	//    - title 非空且不是当前 prompt 的占位副本 → 历史会话已有真实标题，跳过；
	//    - title 是首条消息的占位副本 → 视为未命名，继续生成。
	if title, override, found := store.GetZedDBTitles(sessionID, t.getZedDBPath()); found {
		if override != "" {
			t.markTitled(sessionID, override)
			log.Printf("[AGY Titler] 会话 %s 已由用户手动命名（%s），跳过自动标题", sessionID, override)
			return
		}
		if title != "" && !isPlaceholderTitle(title, prompt) {
			t.markTitled(sessionID, title)
			log.Printf("[AGY Titler] 会话 %s 已存在历史标题（%s），跳过自动标题", sessionID, title)
			return
		}
	}

	// 4. 确认需要生成，占用请求槽位并计入尝试次数
	t.mu.Lock()
	if t.titledSessions[sessionID] || t.activeRequests[sessionID] || t.attempts[sessionID] >= maxTitleAttempts {
		t.mu.Unlock()
		return
	}
	t.activeRequests[sessionID] = true
	t.attempts[sessionID]++
	attempt := t.attempts[sessionID]
	t.mu.Unlock()

	go t.generateAndInjectTitle(sessionID, prompt, attempt)
}

func (t *Titler) generateAndInjectTitle(sessionID, prompt string, attempt int) {
	defer func() {
		t.mu.Lock()
		delete(t.activeRequests, sessionID)
		t.mu.Unlock()
	}()

	title, err := t.CallLLMDirect(prompt)
	if err != nil || title == "" {
		log.Printf("[AGY Titler] 会话 %s 第 %d/%d 次生成失败: %v", sessionID, attempt, maxTitleAttempts, err)
		return
	}

	if err := t.injector.InjectTitle(sessionID, title); err != nil {
		log.Printf("[AGY Titler] 会话 %s 标题注入失败: %v", sessionID, err)
		return
	}

	t.markTitled(sessionID, title)
	log.Printf("[AGY Titler] 会话 %s 标题已生成: %s（第 %d 次尝试）", sessionID, title, attempt)
}

// markTitled 在内存中标记会话已命名，并按需写入持久化存储。
func (t *Titler) markTitled(sessionID, title string) {
	t.mu.Lock()
	t.titledSessions[sessionID] = true
	t.mu.Unlock()

	if title == "" || t.store == nil {
		return
	}
	if err := t.store.MarkTitled(sessionID, title); err != nil {
		log.Printf("[AGY Titler] 持久化会话标题失败: %v", err)
	}
}

// isPlaceholderTitle 判断 Zed 记录的 title 是否只是当前 prompt 的占位副本。
//
// Zed 在用户发出首条消息时就把消息原文（可能截断、可能带文件引用前缀）写入
// sidebar_threads.title。占位标题与 prompt 的归一化文本互为子串；而历史会话中由
// 代理注入的真实标题（如“池化逻辑解析”）与后续追问（如“继续”）通常不存在包含关系。
func isPlaceholderTitle(title, prompt string) bool {
	normalizedTitle := normalizeForCompare(title)
	if normalizedTitle == "" {
		return true
	}
	normalizedPrompt := normalizeForCompare(prompt)
	if normalizedPrompt == "" {
		return false
	}
	return strings.Contains(normalizedPrompt, normalizedTitle) || strings.Contains(normalizedTitle, normalizedPrompt)
}

// normalizeForCompare 去除空白、标点与符号并统一小写，用于宽松比较标题与消息文本。
func normalizeForCompare(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		sb.WriteRune(unicode.ToLower(r))
	}
	return sb.String()
}

// CallLLMDirect 按优先级顺序尝试已配置的模型提供方。
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
		log.Printf("[AGY Titler] provider %s 调用失败，切换下一个: %v", p.Name, err)
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
