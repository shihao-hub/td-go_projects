// Package api 实现智谱 GLM（bigmodel）编码套餐用量查询客户端。
// 接口已实测：GET /api/monitor/usage/quota/limit，Authorization 头携带
// API token（带不带 Bearer 前缀均可，本包统一带 Bearer）。
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL 国内版上游地址。
const DefaultBaseURL = "https://open.bigmodel.cn"

// EnvBaseURL 允许用环境变量覆盖上游地址（测试注入 httptest；将来可指到
// z.ai 国际版）。空值时用 DefaultBaseURL。
const EnvBaseURL = "GLM_QUOTA_WATCH_API_BASE"

// Client 用量查询客户端。
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client // nil 时用 http.DefaultClient
}

// NewClient 构造客户端；baseURL 为空时用 DefaultBaseURL。
func NewClient(baseURL, token string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token}
}

// HTTPError 上游返回非 2xx。
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	body := e.Body
	if len(body) > 300 {
		body = body[:300] + "..."
	}
	return fmt.Sprintf("上游 HTTP %d: %s", e.Status, body)
}

// APIError HTTP 200 但业务码非 200 / success=false。
type APIError struct {
	Code int
	Msg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("上游业务错误 code=%d: %s", e.Code, e.Msg)
}

// Usage data 字段内容：套餐等级 + 各额度窗口。
type Usage struct {
	Level  string  `json:"level"`
	Limits []Limit `json:"limits"`
}

// Limit 单个额度窗口。数值字段全部用指针容忍上游演进
// （实测 TOKENS_LIMIT 只有 percentage/nextResetTime，无 usage/remaining）。
type Limit struct {
	Type          string        `json:"type"` // TOKENS_LIMIT（token 窗口）/ TIME_LIMIT（MCP 调用次数）
	Unit          int           `json:"unit"` // 实测 3=小时、5=月
	Number        int           `json:"number"`
	Percentage    *int64        `json:"percentage"`
	Usage         *float64      `json:"usage"`
	CurrentValue  *float64      `json:"currentValue"`
	Remaining     *float64      `json:"remaining"`
	NextResetTime *int64        `json:"nextResetTime"` // 毫秒时间戳，可能缺失
	UsageDetails  []UsageDetail `json:"usageDetails"`
}

// UsageDetail 分模型用量明细（仅 TIME_LIMIT 实测出现）。
type UsageDetail struct {
	ModelCode string  `json:"modelCode"`
	Usage     float64 `json:"usage"`
}

// Key 返回窗口稳定键 Type:Unit:Number，用作告警状态的记账桶。
func (l Limit) Key() string {
	return fmt.Sprintf("%s:%d:%d", l.Type, l.Unit, l.Number)
}

// TokensLimits 返回 token 类窗口（一期只对这类做阈值告警）。
func (u *Usage) TokensLimits() []Limit {
	var out []Limit
	for _, l := range u.Limits {
		if l.Type == "TOKENS_LIMIT" {
			out = append(out, l)
		}
	}
	return out
}

// rawEnvelope 顶层响应壳；data 保留原文供采样历史原样落盘。
type rawEnvelope struct {
	Code    int             `json:"code"`
	Msg     string          `json:"msg"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}

// FetchUsage 查询当前用量，返回解析结果与 data 字段原文。
func (c *Client) FetchUsage(ctx context.Context) (*Usage, json.RawMessage, error) {
	if c.HTTP == nil {
		c.HTTP = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/monitor/usage/quota/limit", nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("请求用量接口失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil, &HTTPError{Status: resp.StatusCode, Body: string(body)}
	}

	var env rawEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if env.Code != 200 || !env.Success {
		return nil, nil, &APIError{Code: env.Code, Msg: env.Msg}
	}

	usage := &Usage{}
	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, usage); err != nil {
			return nil, nil, fmt.Errorf("解析 data 失败: %w", err)
		}
	}
	return usage, env.Data, nil
}

// Timeout 返回带超时的 ctx 辅助（采样统一 30s 预算）。
func Timeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}
