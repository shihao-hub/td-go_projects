package guiapp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"winstartupmanager/internal/store"
)

const (
	windowStateFileName  = "window.json"
	defaultWindowWidth   = 1040
	defaultWindowHeight  = 680
	minWindowWidth       = 820
	minWindowHeight      = 520
	windowStateSaveDelay = 700 * time.Millisecond
)

// windowState 是窗口几何的持久化快照，单位与 WebviewWindowOptions 一致（DIP）。
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
	dir, err := store.DataDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, windowStateFileName)
}

func loadWindowState() windowState {
	return loadWindowStateFrom(windowStatePath())
}

// loadWindowStateFrom 读取快照；文件缺失、JSON 脏、尺寸过小一律回落默认值。
func loadWindowStateFrom(path string) windowState {
	if path == "" {
		return defaultWindowState()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultWindowState()
	}
	var state windowState
	if err := json.Unmarshal(data, &state); err != nil {
		return defaultWindowState()
	}
	if state.Width < minWindowWidth || state.Height < minWindowHeight {
		return defaultWindowState()
	}
	return state
}

func writeWindowState(state windowState) error {
	return writeWindowStateTo(windowStatePath(), state)
}

func writeWindowStateTo(path string, state windowState) error {
	if path == "" {
		return errors.New("窗口状态路径为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}
