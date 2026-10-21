package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// 窗口几何状态持久化：尺寸 / 位置 / 是否最大化。
//
// 为什么不依赖 Wails：wails v3 (beta.33) 不提供窗口状态记忆选项，窗口每次
// 按 WebviewWindowOptions 里的 Width/Height 重建，因此必须自己落盘。
// 状态文件与数据库同目录（dev / prod 通道天然隔离），坐标与尺寸均为 DIP 逻辑像素，
// 与 WebviewWindowOptions.Width/Height/X/Y 的单位一致，无需换算。

const (
	// windowStateFileName 窗口几何快照文件名。
	windowStateFileName = "window.json"

	// 首次启动的默认尺寸，与历史行为保持一致。
	defaultWindowWidth  = 1280
	defaultWindowHeight = 850

	// 最小可恢复尺寸，低于此值说明读到的是脏数据，回落到默认值。
	minWindowWidth  = 900
	minWindowHeight = 600

	// 窗口几何变更的落盘防抖窗口：拖拽 / 缩放会连续触发大量事件。
	windowStateSaveDelay = 700
)

// windowState 一次窗口几何快照。
type windowState struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	HasPos    bool `json:"hasPosition"`
	Maximised bool `json:"maximised"`
}

// defaultWindowState 无历史记录时的初始状态。
func defaultWindowState() windowState {
	return windowState{Width: defaultWindowWidth, Height: defaultWindowHeight}
}

// windowStatePath 返回窗口状态文件路径，数据目录不可用时返回空串。
func windowStatePath() string {
	dir := appDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, windowStateFileName)
}

// loadWindowState 读取历史快照；缺失或损坏时静默回落默认尺寸。
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

// writeWindowState 把快照落盘（Go 默认写无 BOM UTF-8）。
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
