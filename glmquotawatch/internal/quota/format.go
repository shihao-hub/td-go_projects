package quota

import (
	"fmt"
	"time"

	"glmquotawatch/internal/api"
)

// FormatResetIn 把距下次刷新的时长格式化为人读短文本：
// 4h43m / 37m / 1d2h / 25d。d<=0 返回空串（由调用方决定省略后半句）。
func FormatResetIn(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d >= 24*time.Hour {
		days := int(d / (24 * time.Hour))
		hours := int((d % (24 * time.Hour)) / time.Hour)
		if hours > 0 {
			return fmt.Sprintf("%dd%dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	}
	d = d.Round(time.Minute)
	if d == 0 {
		d = time.Minute // 不足 1 分钟按 1m 展示
	}
	hours := int(d / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// WindowLabel 窗口人读标签。实测 unit=3 表示小时（如 5h 窗口）、
// unit=5 表示月（MCP 月度额度）；其他形态退化为数字组合保证可读。
func WindowLabel(l api.Limit) string {
	switch l.Unit {
	case 3:
		return fmt.Sprintf("%dh 窗口", l.Number)
	case 5:
		return fmt.Sprintf("%d 个月窗口", l.Number)
	default:
		return fmt.Sprintf("%d×unit%d 窗口", l.Number, l.Unit)
	}
}
