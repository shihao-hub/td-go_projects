package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"taskbarguard/internal/core"
)

// 窗口几何状态持久化：Wails v3 不提供窗口状态记忆，必须自己落盘。
// 状态文件放通道数据目录，dev / prod 天然隔离；坐标与尺寸均为 DIP。

const (
	windowStateFileName  = "window.json"
	defaultWindowWidth   = 1180
	defaultWindowHeight  = 780
	minWindowWidth       = 880
	minWindowHeight      = 560
	windowStateSaveDelay = 700
)

type windowState struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	HasPos    bool `json:"hasPosition"`
	Maximised bool `json:"maximised"`
}

func defaultWindowState() windowState {
	return windowState{Width: defaultWindowWidth, Height: defaultWindowHeight}
}

func windowStatePath() string {
	dir := appDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, windowStateFileName)
}

func loadWindowState() windowState {
	state := defaultWindowState()
	path := windowStatePath()
	if path == "" {
		return state
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return state
	}

	var saved windowState
	if err := json.Unmarshal(raw, &saved); err != nil {
		logf("解析窗口状态失败，使用默认尺寸: %v", err)
		return state
	}

	if saved.Width >= minWindowWidth && saved.Height >= minWindowHeight {
		state.Width, state.Height = saved.Width, saved.Height
	}
	state.Maximised = saved.Maximised
	if saved.HasPos {
		state.X, state.Y, state.HasPos = saved.X, saved.Y, true
	}
	return state
}

func writeWindowState(state windowState) error {
	path := windowStatePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// appDataDir 返回当前通道的数据目录（core 是路径的唯一真源）。
func appDataDir() string {
	return core.DataDir(coreChannel())
}
