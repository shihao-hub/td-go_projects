package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"ccquota/internal/ccswitch"
	"ccquota/internal/cred"
	"ccquota/internal/model"
)

// queryOpenCode 查询 OpenCode Go 的三段滚动窗口（5 小时 / 周 / 月）。
// 端点未公开文档化，逐窗口防御解析：任一档缺失只跳过该档。
func queryOpenCode(ctx context.Context, apiKey string) ([]model.Tier, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("缺少 API Key（请在 cc-switch 中配置该供应商）")
	}
	body, status, err := getJSON(ctx, "https://opencode.ai/zen/go/v1/usage", map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if status == 403 {
		return nil, fmt.Errorf("Key 有效但没有 OpenCode Go 订阅（HTTP 403）")
	}
	if err != nil {
		return nil, err
	}

	var payload struct {
		Usage map[string]struct {
			Percent  any `json:"percent"`
			ResetsAt any `json:"resetsAt"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 OpenCode Go 响应失败: %w", err)
	}

	windows := []struct {
		key  string
		name string
	}{
		{"rolling", model.TierFiveHour},
		{"weekly", model.TierWeekly},
		{"monthly", model.TierMonthly},
	}
	var tiers []model.Tier
	for _, w := range windows {
		win, ok := payload.Usage[w.key]
		if !ok {
			continue
		}
		percent, ok := asFloat(win.Percent)
		if !ok {
			continue
		}
		tier := model.Tier{Key: w.name, Utilization: percent}
		// percent 为 0 时上游回的 resetsAt 是「now+窗口时长」的占位值，丢弃。
		if percent > 0 {
			tier.ResetsAt = parseFlexTime(win.ResetsAt)
		}
		tiers = append(tiers, tier)
	}
	return tiers, nil
}

// queryDeepSeek 查询 DeepSeek 官方余额（CNY / USD）。
func queryDeepSeek(ctx context.Context, apiKey string) (*model.Balance, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("缺少 API Key（请在 cc-switch 中配置该供应商）")
	}
	body, status, err := getJSON(ctx, "https://api.deepseek.com/user/balance", map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if status == 401 {
		return nil, fmt.Errorf("认证失败（HTTP 401），请检查 API Key")
	}
	if err != nil {
		return nil, err
	}

	var payload struct {
		BalanceInfos []struct {
			Currency     string `json:"currency"`
			TotalBalance string `json:"total_balance"`
		} `json:"balance_infos"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 DeepSeek 余额响应失败: %w", err)
	}
	if len(payload.BalanceInfos) == 0 {
		return nil, fmt.Errorf("响应中没有余额条目")
	}
	info := payload.BalanceInfos[0]
	value, ok := asFloat(info.TotalBalance)
	if !ok {
		return nil, fmt.Errorf("余额字段无法解析")
	}
	unit := info.Currency
	if unit == "" {
		unit = "CNY"
	}
	return &model.Balance{Value: value, Unit: unit}, nil
}

// queryKimi 查询 Kimi Code 的 5 小时与周窗口。
func queryKimi(ctx context.Context, p ccswitch.Provider) ([]model.Tier, error) {
	token, err := cred.LoadKimiToken(p.ID)
	if err != nil {
		return nil, err
	}
	body, status, err := getJSON(ctx, "https://api.kimi.com/coding/v1/usages", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if status == 401 || status == 403 {
		return nil, fmt.Errorf("认证失败（HTTP %d），请在 cc-switch 授权中心重新登录", status)
	}
	if err != nil {
		return nil, err
	}

	var payload struct {
		Limits []struct {
			Detail struct {
				Limit     any `json:"limit"`
				Remaining any `json:"remaining"`
				ResetTime any `json:"resetTime"`
			} `json:"detail"`
		} `json:"limits"`
		Usage struct {
			Limit     any `json:"limit"`
			Remaining any `json:"remaining"`
			ResetTime any `json:"resetTime"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 Kimi 响应失败: %w", err)
	}

	var tiers []model.Tier
	for _, item := range payload.Limits {
		if tier, ok := ratioTier(model.TierFiveHour, item.Detail.Limit, item.Detail.Remaining, item.Detail.ResetTime); ok {
			tiers = append(tiers, tier)
		}
	}
	if tier, ok := ratioTier(model.TierWeekly, payload.Usage.Limit, payload.Usage.Remaining, payload.Usage.ResetTime); ok {
		tiers = append(tiers, tier)
	}
	return tiers, nil
}

// ratioTier 由 limit/remaining 计算已用百分比。
func ratioTier(key string, limitRaw, remainingRaw, resetRaw any) (model.Tier, bool) {
	limit, ok1 := asFloat(limitRaw)
	remaining, ok2 := asFloat(remainingRaw)
	if !ok1 || !ok2 || limit <= 0 {
		return model.Tier{}, false
	}
	used := limit - remaining
	if used < 0 {
		used = 0
	}
	return model.Tier{
		Key:         key,
		Utilization: used / limit * 100,
		ResetsAt:    parseFlexTime(resetRaw),
	}, true
}

// queryCodex 查询 ChatGPT 订阅的 Codex 额度（wham/usage）。
func queryCodex(ctx context.Context) ([]model.Tier, error) {
	token, err := cred.LoadCodexToken()
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Authorization": "Bearer " + token.AccessToken,
		"User-Agent":    "codex-cli",
	}
	if token.AccountID != "" {
		headers["ChatGPT-Account-Id"] = token.AccountID
	}
	body, status, err := getJSON(ctx, "https://chatgpt.com/backend-api/wham/usage", headers)
	if status == 401 || status == 403 {
		return nil, fmt.Errorf("登录已过期（HTTP %d），请在 cc-switch 授权中心重新登录", status)
	}
	if err != nil {
		return nil, err
	}

	var payload struct {
		RateLimit *struct {
			PrimaryWindow   *codexWindow `json:"primary_window"`
			SecondaryWindow *codexWindow `json:"secondary_window"`
		} `json:"rate_limit"`
		PlanType string `json:"plan_type"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 Codex 响应失败: %w", err)
	}
	if payload.RateLimit == nil {
		return nil, nil
	}

	var tiers []model.Tier
	for _, w := range []*codexWindow{payload.RateLimit.PrimaryWindow, payload.RateLimit.SecondaryWindow} {
		if w == nil || w.UsedPercent == nil {
			continue
		}
		name := "unknown"
		if w.LimitWindowSeconds != nil {
			name = windowName(int64(*w.LimitWindowSeconds))
		}
		tier := model.Tier{Key: name, Utilization: *w.UsedPercent}
		// 已用为 0 时上游可能回「now + 窗口时长」的占位时间。
		if *w.UsedPercent > 0 && w.ResetAt != nil {
			tier.ResetsAt = epochToTime(int64(*w.ResetAt))
		}
		tiers = append(tiers, tier)
	}
	return tiers, nil
}

type codexWindow struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds *float64 `json:"limit_window_seconds"`
	ResetAt            *float64 `json:"reset_at"`
}

// queryCommandCode 查询 Command Code：whoami → credits → subscriptions → usage summary。
func queryCommandCode(ctx context.Context, apiKey string) ([]model.Tier, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("缺少 API Key（请在 cc-switch 中配置该供应商）")
	}
	const base = "https://api.commandcode.ai"

	fetch := func(path string, params url.Values) (map[string]any, error) {
		target := base + path
		if len(params) > 0 {
			target += "?" + params.Encode()
		}
		body, status, err := getJSON(ctx, target, map[string]string{
			"Authorization": "Bearer " + apiKey,
		})
		if status == 401 {
			return nil, fmt.Errorf("认证失败（HTTP 401），请检查 API Key")
		}
		if err != nil {
			return nil, err
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("解析 %s 响应失败: %w", path, err)
		}
		return out, nil
	}

	whoami, err := fetch("/alpha/whoami", url.Values{"limits": {"1"}})
	if err != nil {
		return nil, err
	}
	orgID := ""
	if org, ok := whoami["org"].(map[string]any); ok {
		if id, ok := org["id"].(string); ok {
			orgID = id
		}
	}
	orgParams := url.Values{}
	if orgID != "" {
		orgParams.Set("orgId", orgID)
	}

	credits, err := fetch("/alpha/billing/credits", orgParams)
	if err != nil {
		return nil, err
	}
	creditRoot, ok := credits["credits"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("响应缺少 credits 字段")
	}

	subParams := url.Values{}
	for k, v := range orgParams {
		subParams[k] = v
	}
	subscription, subErr := fetch("/alpha/billing/subscriptions", subParams)
	if subErr != nil {
		subscription = nil
	}

	summaryParams := url.Values{}
	for k, v := range orgParams {
		summaryParams[k] = v
	}
	if subscription != nil {
		if data, ok := subscription["data"].(map[string]any); ok {
			if since, ok := data["currentPeriodStart"].(string); ok && since != "" {
				summaryParams.Set("since", since)
			}
		}
	}
	summary, err := fetch("/alpha/usage/summary", summaryParams)
	if err != nil {
		return nil, err
	}

	var tiers []model.Tier
	monthlyRemaining := maxFloat(creditRoot["monthlyCredits"], 0)
	purchasedRemaining := maxFloat(creditRoot["purchasedCredits"], 0)
	freeRemaining := maxFloat(creditRoot["freeCredits"], 0)
	remainingTotal := monthlyRemaining + purchasedRemaining + freeRemaining
	spent, ok := asFloat(summary["totalCost"])
	if !ok {
		return nil, fmt.Errorf("响应缺少 totalCost 字段")
	}
	if spent < 0 {
		spent = 0
	}
	pool := spent + remainingTotal

	windowLimits, ok := credits["windowLimits"].(map[string]any)
	if !ok {
		windowLimits, _ = creditRoot["windowLimits"].(map[string]any)
	}
	if windowLimits != nil {
		if limited, ok := windowLimits["limited"].(bool); ok && limited {
			if tier, ok := commandCodeWindow(windowLimits["fiveHour"], model.TierFiveHour); ok {
				tiers = append(tiers, tier)
			}
			if tier, ok := commandCodeWindow(windowLimits["weekly"], model.TierWeekly); ok {
				tiers = append(tiers, tier)
			}
		}
	}

	monthly := model.Tier{Key: model.TierMonthly}
	if pool > 0 {
		monthly.Utilization = spent / pool * 100
	}
	if subscription != nil {
		if data, ok := subscription["data"].(map[string]any); ok {
			monthly.ResetsAt = parseFlexTime(data["currentPeriodEnd"])
		}
	}
	tiers = append(tiers, monthly)
	return tiers, nil
}

// commandCodeWindow 解析 {used, cap, resetAt} 形态的窗口。
func commandCodeWindow(raw any, key string) (model.Tier, bool) {
	win, ok := raw.(map[string]any)
	if !ok {
		return model.Tier{}, false
	}
	used, ok1 := asFloat(win["used"])
	capacity, ok2 := asFloat(win["cap"])
	if !ok1 || !ok2 || capacity <= 0 {
		return model.Tier{}, false
	}
	return model.Tier{
		Key:         key,
		Utilization: used / capacity * 100,
		ResetsAt:    parseFlexTime(win["resetAt"]),
	}, true
}

func maxFloat(raw any, floor float64) float64 {
	if v, ok := asFloat(raw); ok && v > floor {
		return v
	}
	return floor
}
