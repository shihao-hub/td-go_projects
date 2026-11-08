package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"rebootremind/internal/fsutil"
)

// StateFile 是延迟提醒与最近动作的持久化文件名。
const StateFile = "state.json"

// 为便于诊断的动作名。
const (
	ActionSnoozeShort = "snooze_short"
	ActionSnoozeLong  = "snooze_long"
	ActionRestart     = "restart"
	ActionDismissed   = "dismissed"
)

// State 是跨进程共享的提醒状态。
type State struct {
	SnoozeUntil    *time.Time `json:"snooze_until,omitempty"`
	LastNotifiedAt *time.Time `json:"last_notified_at,omitempty"`
	LastAction     string     `json:"last_action,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// LoadState 读取状态；文件不存在时返回零值。
func LoadState(dir string) (State, error) {
	data, err := os.ReadFile(filepath.Join(dir, StateFile))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

// SaveState 以原子方式持久化状态。
func SaveState(dir string, state State) error {
	state.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fsutil.WriteFileAtomic(filepath.Join(dir, StateFile), data)
}

// Snooze 写入延迟提醒截止时间与动作名。
func Snooze(state *State, now time.Time, duration time.Duration, action string) {
	until := now.Add(duration)
	state.SnoozeUntil = &until
	state.LastAction = action
}
