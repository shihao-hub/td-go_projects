package ui

// Action 表示用户在提醒弹窗上的选择。
type Action int

const (
	// ActionDismissed 表示用户关闭了弹窗（视同稍后提醒）。
	ActionDismissed Action = iota
	// ActionSnoozeShort 表示 4 小时后再提醒。
	ActionSnoozeShort
	// ActionSnoozeLong 表示 24 小时后再提醒。
	ActionSnoozeLong
	// ActionRestart 表示用户请求立即重启。
	ActionRestart
)

// String 返回动作的稳定标识。
func (a Action) String() string {
	switch a {
	case ActionSnoozeShort:
		return "snooze_short"
	case ActionSnoozeLong:
		return "snooze_long"
	case ActionRestart:
		return "restart"
	default:
		return "dismissed"
	}
}
