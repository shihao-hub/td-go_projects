package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type windowState struct {
	Width       int  `json:"width"`
	Height      int  `json:"height"`
	X           int  `json:"x"`
	Y           int  `json:"y"`
	IsMaximized bool `json:"is_maximized"`
}

const (
	defaultWinWidth  = 1100
	defaultWinHeight = 720
	minWinWidth      = 800
	minWinHeight     = 600
)

func defaultWindowState() windowState {
	return windowState{
		Width:       defaultWinWidth,
		Height:      defaultWinHeight,
		X:           -1,
		Y:           -1,
		IsMaximized: false,
	}
}

func windowStatePath() string {
	dir := appDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "window.json")
}

func loadWindowState() windowState {
	def := defaultWindowState()
	p := windowStatePath()
	if p == "" {
		return def
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return def
	}
	var state windowState
	if err := json.Unmarshal(data, &state); err != nil {
		return def
	}
	if state.Width < minWinWidth {
		state.Width = defaultWinWidth
	}
	if state.Height < minWinHeight {
		state.Height = defaultWinHeight
	}
	// 防呆检测：若离屏坐标太离谱，恢复默认居中
	if state.X < -1000 || state.Y < -1000 || state.X > 20000 || state.Y > 20000 {
		state.X = -1
		state.Y = -1
	}
	return state
}

func saveWindowState(state windowState) {
	p := windowStatePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o644)
}
