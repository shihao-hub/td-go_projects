package model

import "fmt"

// FormatUptime 把秒数格式化为中文可读的开机时长。
func FormatUptime(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	switch {
	case days > 0:
		return fmt.Sprintf("%d 天 %d 小时", days, hours)
	case hours > 0:
		return fmt.Sprintf("%d 小时 %d 分", hours, minutes)
	default:
		return fmt.Sprintf("%d 分", minutes)
	}
}
