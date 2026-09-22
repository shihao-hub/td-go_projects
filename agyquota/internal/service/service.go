// Package service 是 agyquota 的公共业务核心：凭据读取、配额获取、
// 解析归一化与业务错误都归属这里；CLI 与 MCP 只是两个入口适配器。
// 本服务无状态、零落盘（除网络栈内部缓存外不写任何文件）。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"agyquota/internal/agapi"
)

// 稳定业务错误码（公开契约）。
const (
	ErrTokenFileMissing = "token_file_missing" // 凭据文件不存在
	ErrTokenParse       = "token_parse_failed" // 凭据文件缺失字段或不可解析
	ErrAuth             = "auth_failed"        // OAuth 刷新失败
	ErrRateLimited      = "rate_limited"       // 配额接口 429（重试后仍失败）
	ErrAPI              = "api_error"          // 配额接口其他失败
	ErrParse            = "response_parse_failed"
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
	Window            string   `json:"window"`                      // 归一化标识：five_hour / weekly / daily / 接口原值 / "quota"
	Label             string   `json:"label"`                       // 人读短标签：5h / weekly / …
	Percent           float64  `json:"percent"`                     // 剩余百分比 0..100（保留 1 位小数）
	RemainingFraction *float64 `json:"remainingFraction,omitempty"` // 接口原始小数
	ResetAt           string   `json:"resetAt,omitempty"`           // 重置时间（尽量 RFC3339，否则接口原值）
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
	TokenFile string // 空则用默认凭据路径
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

// GetQuota 查询配额：读凭据 → 刷新 access token → 调配额接口 → 归一化。
func (s *Service) GetQuota(ctx context.Context, opt Options) (*Snapshot, error) {
	tf, err := s.loadCreds(opt)
	if err != nil {
		return nil, err
	}
	s.progress("已加载凭据（%s），正在刷新 Google 访问令牌…", tf.ProjectID)
	token, err := s.refresh(ctx, tf)
	if err != nil {
		return nil, err
	}
	s.progress("正在查询配额接口（遇限流自动退避重试，最长约 2.5 分钟）…")
	raw, err := s.fetch(ctx, token, tf)
	if err != nil {
		return nil, err
	}
	snap, err := parseSnapshot(raw, time.Now())
	if err != nil {
		return nil, withSuggestion(
			errf(ErrParse, "解析配额接口响应失败：%v", err),
			"接口响应结构可能已变化，可执行 agyquota quota --raw 查看原始响应",
		)
	}
	return snap, nil
}

// FetchRaw 返回配额接口原始响应（--raw 调试用）。
func (s *Service) FetchRaw(ctx context.Context, opt Options) (json.RawMessage, error) {
	tf, err := s.loadCreds(opt)
	if err != nil {
		return nil, err
	}
	token, err := s.refresh(ctx, tf)
	if err != nil {
		return nil, err
	}
	return s.fetch(ctx, token, tf)
}

func (s *Service) loadCreds(opt Options) (*agapi.TokenFile, error) {
	path := opt.TokenFile
	if path == "" {
		p, err := agapi.DefaultTokenPath()
		if err != nil {
			return nil, errf(ErrTokenFileMissing, "无法定位 Antigravity 凭据目录：%v", err)
		}
		path = p
	}
	tf, err := agapi.LoadTokenFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, withSuggestion(
				errf(ErrTokenFileMissing, "未找到 Antigravity 凭据文件（%s）", path),
				"请先在 Antigravity IDE 或 Zed ACP antigravity agent 中登录一次，凭据会自动落盘",
			)
		}
		return nil, withSuggestion(
			errf(ErrTokenParse, "%v", err),
			"凭据文件损坏时，重新登录 Antigravity 可重新生成",
		)
	}
	if tf.ProjectID == "" {
		return nil, errf(ErrTokenParse, "凭据文件缺少 project_id 字段")
	}
	return tf, nil
}

func (s *Service) refresh(ctx context.Context, tf *agapi.TokenFile) (string, error) {
	token, err := agapi.RefreshAccessToken(ctx, tf)
	if err != nil {
		return "", withSuggestion(
			errf(ErrAuth, "刷新 Google 凭据失败：%v", err),
			"refresh_token 可能已失效，重新登录 Antigravity 后重试",
		)
	}
	return token, nil
}

func (s *Service) fetch(ctx context.Context, token string, tf *agapi.TokenFile) (json.RawMessage, error) {
	raw, err := s.api.FetchAvailableModels(ctx, token, tf.ProjectID)
	if err != nil {
		var rle *agapi.RateLimitError
		if errors.As(err, &rle) {
			return nil, withSuggestion(
				errf(ErrRateLimited, "%v", err),
				"该接口限流较严：关闭正在运行的 Antigravity IDE 后重试，或稍等几分钟再试",
			)
		}
		return nil, withSuggestion(
			errf(ErrAPI, "%v", err),
			"Google 内部接口可能已变化；可打开 Antigravity IDE 的 Settings → Models & Usage 查看配额",
		)
	}
	return raw, nil
}
