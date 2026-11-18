// Package quota 执行各供应商的额度查询，并把结果归一化成 model.Card。
//
// 设计约束：
//   - 逐窗口防御解析：单个窗口缺失或形态变化时跳过该窗口，不整体失败；
//   - 任何凭据都不进日志与错误文本（用 cred.Mask 掩码）；
//   - 官方订阅类只读 token，绝不刷新或回写。
package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ccquota/internal/ccswitch"
	"ccquota/internal/model"
)

// httpTimeout 是单个额度请求的超时（对齐 cc-switch 的 15 秒）。
const httpTimeout = 15 * time.Second

var httpClient = &http.Client{Timeout: httpTimeout}

// Kind 判定供应商应走哪个查询器；返回空表示 P0 不支持。
func Kind(p ccswitch.Provider) string {
	switch p.AppType {
	case "agy":
		return "agy"
	case "kimi":
		return "kimi"
	case "codex":
		return "codex"
	case "claude":
		if p.Usage == nil {
			return ""
		}
		switch p.Usage.CodingPlanProvider {
		case "opencode_go":
			return "opencode"
		case "command_code":
			return "command_code"
		}
		if p.Usage.TemplateType == "balance" {
			return "deepseek"
		}
		if strings.Contains(p.BaseURL, "deepseek.com") {
			return "deepseek"
		}
	}
	return ""
}

// Card 的显示名称与类型由查询器类型决定。
func cardFor(p ccswitch.Provider, kind string) model.Card {
	card := model.Card{
		App:        p.AppType,
		ProviderID: p.ID,
		Name:       p.Name,
		BaseURL:    p.BaseURL,
		Current:    p.IsCurrent,
	}
	switch kind {
	case "deepseek":
		card.Kind = model.KindBalance
	default:
		card.Kind = model.KindCodingPlan
	}
	return card
}

// Query 查询单个供应商；错误写入 Card.Err，不返回 error（便于批量聚合）。
func Query(ctx context.Context, p ccswitch.Provider) model.Card {
	kind := Kind(p)
	card := cardFor(p, kind)
	card.QueriedAt = time.Now()

	if kind == "" {
		card.Err = "未启用可识别的用量查询"
		return card
	}

	var (
		tiers   []model.Tier
		balance *model.Balance
		err     error
	)
	switch kind {
	case "opencode":
		tiers, err = queryOpenCode(ctx, p.APIKey)
	case "deepseek":
		balance, err = queryDeepSeek(ctx, p.APIKey)
	case "kimi":
		tiers, err = queryKimi(ctx, p)
	case "codex":
		tiers, err = queryCodex(ctx)
	case "command_code":
		tiers, err = queryCommandCode(ctx, p.APIKey)
	case "agy":
		tiers, err = queryAgy(ctx)
	}

	if err != nil {
		card.Err = err.Error()
		return card
	}
	if len(tiers) == 0 && balance == nil {
		card.Err = "响应形态变化，未解析到任何额度窗口"
		return card
	}
	card.OK = true
	card.Tiers = tiers
	card.Balance = balance
	return card
}

// getJSON 发起 GET 并把响应体读入内存；非 2xx 时返回带状态码的错误。
func getJSON(ctx context.Context, url string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("构造请求失败: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("网络请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, resp.StatusCode, nil
}

// parseFlexTime 宽松解析时间：支持 RFC3339 字符串、Unix 秒、Unix 毫秒。
func parseFlexTime(v any) *time.Time {
	switch raw := v.(type) {
	case string:
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return &t
		}
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return &t
		}
		return nil
	case float64:
		return epochToTime(int64(raw))
	case json.Number:
		n, err := raw.Int64()
		if err != nil {
			return nil
		}
		return epochToTime(n)
	default:
		return nil
	}
}

// epochToTime 按量级判断秒/毫秒。
func epochToTime(n int64) *time.Time {
	if n <= 0 {
		return nil
	}
	if n > 1e11 { // 毫秒
		t := time.UnixMilli(n)
		return &t
	}
	t := time.Unix(n, 0)
	return &t
}

// asFloat 把 JSON 数字/字符串统一转成 float64。
func asFloat(v any) (float64, bool) {
	switch raw := v.(type) {
	case float64:
		return raw, true
	case json.Number:
		f, err := raw.Float64()
		return f, err == nil
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%f", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

// windowName 把窗口秒数映射成统一档位名。
func windowName(seconds int64) string {
	switch {
	case seconds <= 6*3600:
		return model.TierFiveHour
	case seconds <= 8*24*3600:
		return model.TierWeekly
	default:
		return model.TierMonthly
	}
}

// remainToTier 由「剩余比例」构造档位（agy / kimi 的语义）。
func remainToTier(key string, remainingFraction float64, resetsAt *time.Time) model.Tier {
	util := (1 - remainingFraction) * 100
	if util < 0 {
		util = 0
	}
	return model.Tier{Key: key, Utilization: util, ResetsAt: resetsAt}
}
