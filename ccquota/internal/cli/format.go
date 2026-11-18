package cli

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"ccquota/internal/model"
)

// appTitles 是 app_type 到界面标题的映射。
var appTitles = map[string]string{
	"claude": "Claude Code",
	"codex":  "Codex",
	"kimi":   "Kimi Code",
	"agy":    "Antigravity",
}

// tierTitles 是统一档位名的中文标签。
var tierTitles = map[string]string{
	model.TierFiveHour: "5 小时",
	model.TierWeekly:   "本周",
	model.TierMonthly:  "本月",
}

// appTitle 返回分组标题。
func appTitle(app string) string {
	if t, ok := appTitles[app]; ok {
		return t
	}
	if app == "" {
		return "其他"
	}
	return app
}

// tierTitle 返回档位标签：优先用 Tier.Label（Antigravity 的模型分组），否则用中文窗口名。
func tierTitle(t model.Tier) string {
	if t.Label != "" {
		if name, ok := tierTitles[t.Key]; ok {
			return t.Label + " · " + name
		}
		return t.Label
	}
	if name, ok := tierTitles[t.Key]; ok {
		return name
	}
	return t.Key
}

// formatPercent 返回剩余百分比（整数）。
func formatPercent(t model.Tier) string {
	return strconv.Itoa(int(math.Round(t.Remaining()))) + "%"
}

// formatCountdown 返回距重置的倒计时；已过期返回「已重置」。
func formatCountdown(resetsAt *time.Time) string {
	if resetsAt == nil {
		return ""
	}
	remain := time.Until(*resetsAt)
	if remain <= 0 {
		return "已重置"
	}
	days := int(remain.Hours()) / 24
	hours := int(remain.Hours()) % 24
	minutes := int(remain.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

// formatBalance 输出余额行。
func formatBalance(b *model.Balance) string {
	if b == nil {
		return ""
	}
	return fmt.Sprintf("%.2f %s", b.Value, b.Unit)
}

// tierSummary 把一档压缩成「5 小时剩余 98%（4h21m 后重置）」。
func tierSummary(t model.Tier) string {
	out := tierTitle(t) + "剩余 " + formatPercent(t)
	switch cd := formatCountdown(t.ResetsAt); cd {
	case "":
	case "已重置":
		out += "（已重置）"
	default:
		out += "（" + cd + "后重置）"
	}
	return out
}
