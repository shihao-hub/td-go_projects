// Package service 是 agyquota 的公共业务核心：配额获取、
// 解析归一化与业务错误都归属于这里；CLI 与 MCP 只是两个入口适配器。
// 本服务无状态、零落盘。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"agyquota/internal/agapi"
)

// 稳定业务错误码（公开契约）。
const (
	ErrAgyNotFound = "agy_not_found"
	ErrAgyExecute  = "agy_execute_failed"
	ErrParse       = "response_parse_failed"
)

// Error 公共业务错误：稳定 code + 可公开 message + 可选建议。
// 不携带入口语义（CLI 退出码 / MCP isError），由入口自行映射。
type Error struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Suggestions []string `json:"suggestions,omitempty"`
}

func (e *Error) Error() string { return e.Message }

func errf(code, format string, a ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...)}
}

func withSuggestion(e *Error, s string) *Error {
	e.Suggestions = append(e.Suggestions, s)
	return e
}

// Window 单个配额窗口（如 5 小时窗口 / 每周窗口）。
type Window struct {
	Window            string   `json:"window"`                      // 归一化标识：five_hour / weekly / daily / "quota"
	Label             string   `json:"label"`                       // 人读短标签：5h / weekly / …
	Percent           float64  `json:"percent"`                     // 剩余百分比 0..100（保留 1 位小数）
	RemainingFraction *float64 `json:"remainingFraction,omitempty"` // 接口原始小数
	ResetAt           string   `json:"resetAt,omitempty"`           // 重置时间（尽量 RFC3339）
	ResetsIn          string   `json:"resetsIn,omitempty"`          // 人读剩余："6天21小时后刷新"
}

// ModelQuota 单模型的配额窗口集合。
type ModelQuota struct {
	Name    string   `json:"name"`
	Windows []Window `json:"windows"`
}

// Bucket 模型桶。
type Bucket struct {
	Name   string       `json:"name"`
	Models []ModelQuota `json:"models"`
}

// Snapshot 一次配额查询结果。
type Snapshot struct {
	FetchedAt time.Time `json:"fetchedAt"`
	Buckets   []Bucket  `json:"buckets"`
}

// Options 查询选项。
type Options struct {
	TokenFile string // 保留兼容字段，agy 自动管理凭证
}

// Service 公共业务服务：无状态、可重入。
type Service struct {
	api *agapi.Client
	// Progress 可选进度回调（阶段与重试提示）；nil 时静默（MCP 入口即保持静默）。
	Progress func(format string, args ...any)
}

// New 创建服务。
func New() *Service {
	svc := &Service{api: agapi.NewClient()}
	svc.api.Logf = svc.progress
	return svc
}

func (s *Service) progress(format string, a ...any) {
	if s.Progress != nil {
		s.Progress(format, a...)
	}
}

// GetQuota 查询配额：调用 agy CLI 获取 /usage 输出并归一化。
func (s *Service) GetQuota(ctx context.Context, opt Options) (*Snapshot, error) {
	s.progress("正在通过 Antigravity CLI (agy) 查询模型配额 ...")
	raw, err := s.api.FetchUsage(ctx)
	if err != nil {
		return nil, withSuggestion(
			errf(ErrAgyExecute, "获取配额失败: %v", err),
			"请确认系统已安装 Antigravity CLI (agy) 并且能正常执行 `agy -p /usage`",
		)
	}

	snap, err := parseSnapshot(raw, time.Now())
	if err != nil {
		return nil, withSuggestion(
			errf(ErrParse, "解析配额响应失败: %v", err),
			"可执行 agyquota quota --raw 查看 agy 原始响应",
		)
	}
	return snap, nil
}

// FetchRaw 返回配额接口原始响应（--raw 调试用）。
func (s *Service) FetchRaw(ctx context.Context, opt Options) (json.RawMessage, error) {
	s.progress("正在通过 Antigravity CLI (agy) 获取原始响应 ...")
	raw, err := s.api.FetchUsage(ctx)
	if err != nil {
		return nil, errf(ErrAgyExecute, "%v", err)
	}
	return raw, nil
}
