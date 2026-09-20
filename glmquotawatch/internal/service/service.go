// Package service 承载 glmquotawatch 的业务用例：参数校验 + store/api/quota
// 编排，输入输出均为纯 Go 类型。不做任何标准流输出与 os.Exit——
// CLI 包络层与 MCP 适配层共用本包，是唯一业务真相源。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"glmquotawatch/internal/api"
	"glmquotawatch/internal/quota"
	"glmquotawatch/internal/store"
)

// Error 业务错误：Code 供 CLI JSON 包络与 MCP IsError 文本共用（格式
// "code: message"），退出码由 CLI 壳决定，不进入公共错误。
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func errf(code, format string, a ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...)}
}

// Notifier 通知抽象：daemon 经它发送告警，测试注入 fake。
type Notifier interface {
	Notify(title, msg string) error
}

// ConfigView 配置视图（token 恒脱敏，任何出口都不回明文）。
type ConfigView struct {
	Token      string `json:"token"` // 脱敏后的展示形态
	HasToken   bool   `json:"has_token"`
	Interval   string `json:"interval"`
	Thresholds []int  `json:"thresholds"`
	Hysteresis int    `json:"hysteresis"`
	Silent     bool   `json:"silent"`
	Dir        string `json:"dir"` // 数据目录（便于用户定位 config/state/samples）
}

// WindowView 单个 token 窗口的当前状态。
type WindowView struct {
	Key           string `json:"key"`
	Type          string `json:"type"`
	Label         string `json:"label"`
	Percentage    int64  `json:"percentage"`
	NextResetTime string `json:"next_reset_time,omitempty"` // RFC3339
	ResetIn       string `json:"reset_in,omitempty"`        // 人读，如 "4h43m"
	Notified      []int  `json:"notified"`                  // 已通知档位（升序）
}

// StatusView 一次采样的完整状态。
type StatusView struct {
	Level     string       `json:"level"`
	SampledAt string       `json:"sampled_at"`
	Windows   []WindowView `json:"windows"`
}

// Service 业务用例集合。
type Service struct {
	st      *store.Store
	baseURL string
}

// Option Service 构造选项。
type Option func(*Service)

// WithBaseURL 覆盖上游 API 地址（测试注入 httptest、将来接国际版）。
func WithBaseURL(u string) Option {
	return func(s *Service) { s.baseURL = u }
}

// New 构造 Service；baseURL 为空时用 api.DefaultBaseURL。
func New(st *store.Store, opts ...Option) *Service {
	s := &Service{st: st, baseURL: api.DefaultBaseURL}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Open 组装入口（CLI/MCP）用：默认数据目录；上游地址可被环境变量
// GLM_QUOTA_WATCH_API_BASE 覆盖。
func Open() (*Service, error) {
	dir, err := store.DefaultDir()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(dir)
	if err != nil {
		return nil, err
	}
	return New(st, WithBaseURL(os.Getenv(api.EnvBaseURL))), nil
}

// ---- 配置类用例 ----

// SetToken 配置 GLM API token（trim 后长度须 ≥ 20）。
func (s *Service) SetToken(token string) (ConfigView, error) {
	token = strings.TrimSpace(token)
	if len(token) < 20 {
		return ConfigView{}, errf("bad_args", "token 长度须 ≥ 20，当前 %d", len(token))
	}
	cfg, err := s.st.LoadConfig()
	if err != nil {
		return ConfigView{}, errf("internal", "读取配置失败: %v", err)
	}
	cfg.Token = token
	if err := s.st.SaveConfig(cfg); err != nil {
		return ConfigView{}, errf("internal", "保存配置失败: %v", err)
	}
	return s.view(cfg), nil
}

// RemoveToken 清除 token；幂等（未配置时也返回成功）。
func (s *Service) RemoveToken() (ConfigView, error) {
	cfg, err := s.st.LoadConfig()
	if err != nil {
		return ConfigView{}, errf("internal", "读取配置失败: %v", err)
	}
	cfg.Token = ""
	if err := s.st.SaveConfig(cfg); err != nil {
		return ConfigView{}, errf("internal", "保存配置失败: %v", err)
	}
	return s.view(cfg), nil
}

// GetConfig 返回当前配置（token 脱敏）。
func (s *Service) GetConfig() (ConfigView, error) {
	cfg, err := s.st.LoadConfig()
	if err != nil {
		return ConfigView{}, errf("internal", "读取配置失败: %v", err)
	}
	return s.view(cfg), nil
}

// configSetKeys config set 支持的键（供错误提示与校验）。
var configSetKeys = []string{"interval", "thresholds", "hysteresis", "silent"}

// SetConfig 修改单个配置键，校验规则：
// interval 正时长 clamp [30s, 24h]；thresholds 逗号分隔 1..99 排序去重；
// hysteresis 0..30；silent 布尔。未知键报 unknown_key 并列出可用键。
func (s *Service) SetConfig(key, value string) (ConfigView, error) {
	cfg, err := s.st.LoadConfig()
	if err != nil {
		return ConfigView{}, errf("internal", "读取配置失败: %v", err)
	}
	switch key {
	case "interval":
		d, perr := time.ParseDuration(strings.TrimSpace(value))
		if perr != nil || d <= 0 {
			return ConfigView{}, errf("bad_value", "interval 须为正时长（如 90s、5m、1h），收到 %q", value)
		}
		cfg.Interval = clampInterval(d).String()
	case "thresholds":
		var ts []int
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			n, nerr := strconv.Atoi(part)
			if nerr != nil || n < 1 || n > 99 {
				return ConfigView{}, errf("bad_value", "thresholds 每项须为 1..99 的整数，收到 %q", part)
			}
			ts = append(ts, n)
		}
		if len(ts) == 0 {
			return ConfigView{}, errf("bad_value", "thresholds 至少一项，如 50,60,80,90")
		}
		cfg.Thresholds = dedupSorted(ts)
	case "hysteresis":
		n, nerr := strconv.Atoi(strings.TrimSpace(value))
		if nerr != nil || n < 0 || n > 30 {
			return ConfigView{}, errf("bad_value", "hysteresis 须为 0..30 的整数，收到 %q", value)
		}
		cfg.Hysteresis = n
	case "silent":
		b, berr := strconv.ParseBool(strings.TrimSpace(value))
		if berr != nil {
			return ConfigView{}, errf("bad_value", "silent 须为 true/false，收到 %q", value)
		}
		cfg.Silent = b
	default:
		return ConfigView{}, errf("unknown_key", "不支持的配置键 %q，可用键：%s", key, strings.Join(configSetKeys, "、"))
	}
	if err := s.st.SaveConfig(cfg); err != nil {
		return ConfigView{}, errf("internal", "保存配置失败: %v", err)
	}
	return s.view(cfg), nil
}

func (s *Service) view(cfg store.Config) ConfigView {
	return ConfigView{
		Token:      MaskToken(cfg.Token),
		HasToken:   cfg.Token != "",
		Interval:   cfg.Interval,
		Thresholds: append([]int{}, cfg.Thresholds...), // 兜底非 nil：thresholds 为 null 时视图仍序列化为 []，与不含 null 的 outputSchema 一致
		Hysteresis: cfg.Hysteresis,
		Silent:     cfg.Silent,
		Dir:        s.st.Dir(),
	}
}

// MaskToken 脱敏：前 8 后 4 中间省略；过短全部打星。
func MaskToken(t string) string {
	if t == "" {
		return ""
	}
	if len(t) <= 12 {
		return strings.Repeat("*", len(t))
	}
	return t[:8] + "…" + t[len(t)-4:]
}

func clampInterval(d time.Duration) time.Duration {
	if d < 30*time.Second {
		return 30 * time.Second
	}
	if d > 24*time.Hour {
		return 24 * time.Hour
	}
	return d
}

func dedupSorted(ts []int) []int {
	sort.Ints(ts)
	out := ts[:0]
	for i, v := range ts {
		if i == 0 || v != ts[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// ---- 采样用例 ----

// SampleOnce 立即采样一次：请求上游 → 原文落历史 → 评估阈值推进告警状态。
// 不发送任何通知（通知是 daemon 的职责）。返回状态视图与逐窗口评估结果
// （CLI/MCP 只用视图，daemon 用评估结果）。
func (s *Service) SampleOnce(ctx context.Context) (StatusView, []quota.Outcome, error) {
	cfg, err := s.st.LoadConfig()
	if err != nil {
		return StatusView{}, nil, errf("internal", "读取配置失败: %v", err)
	}
	if cfg.Token == "" {
		return StatusView{}, nil, errf("no_token", "尚未配置 token，请先执行 token set")
	}

	usage, raw, err := api.NewClient(s.baseURL, cfg.Token).FetchUsage(ctx)
	if err != nil {
		return StatusView{}, nil, mapUpstream(err)
	}

	now := time.Now()
	if _, serr := s.st.AppendSample(now, raw); serr != nil {
		slog.Warn("采样历史写入失败", "err", serr)
	}

	prev, err := s.st.LoadState()
	if err != nil {
		slog.Warn("state.json 损坏，按空状态继续（可能补发一条通知）", "err", err)
		prev = store.State{}
	}
	next, outs := quota.Evaluate(cfg, prev, usage.TokensLimits(), now)
	if err := s.st.SaveState(next); err != nil {
		slog.Warn("state.json 写入失败（下一轮重试，不影响通知）", "err", err)
	}
	return buildStatusView(usage, now, next), outs, nil
}

func buildStatusView(u *api.Usage, now time.Time, st store.State) StatusView {
	v := StatusView{
		Level:     u.Level,
		SampledAt: now.Format(time.RFC3339),
		Windows:   []WindowView{},
	}
	for _, l := range u.TokensLimits() {
		notified := st.Notified[l.Key()]
		if notified == nil {
			notified = []int{} // 序列化为 [] 而非 null
		}
		w := WindowView{
			Key:      l.Key(),
			Type:     l.Type,
			Label:    quota.WindowLabel(l),
			Notified: notified,
		}
		if l.Percentage != nil {
			w.Percentage = *l.Percentage
		}
		if l.NextResetTime != nil && *l.NextResetTime > 0 {
			t := time.UnixMilli(*l.NextResetTime)
			w.NextResetTime = t.Format(time.RFC3339)
			w.ResetIn = quota.FormatResetIn(t.Sub(now))
		}
		v.Windows = append(v.Windows, w)
	}
	return v
}

func mapUpstream(err error) error {
	var he *api.HTTPError
	if errors.As(err, &he) {
		return errf("api_error", "%v", he)
	}
	var ae *api.APIError
	if errors.As(err, &ae) {
		return errf("upstream_error", "%v", ae)
	}
	return errf("internal", "%v", err)
}

// JSONLLine 采样历史行结构（导出仅供文档对照，写入走 store.AppendSample）。
type JSONLLine = struct {
	Ts   string          `json:"ts"`
	Data json.RawMessage `json:"data"`
}
