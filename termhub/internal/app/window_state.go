// window_state.go：主窗口几何与最大化状态记忆（wails v3 不内置）。
package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	windowStateFileName = "window.json"
	defaultWindowWidth  = 1080
	defaultWindowHeight = 720
	minWindowWidth      = 720
	minWindowHeight     = 480
)

// windowState 落盘的主窗口状态；HasPos 为假时保持系统默认居中。
type windowState struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	HasPos    bool `json:"hasPosition"`
	Maximised bool `json:"maximised"`
}

// windowArea 用于判断窗口是否仍落在某块屏幕的工作区内。
type windowArea struct {
	X      int
	Y      int
	Width  int
	Height int
}

func windowStatePath(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, windowStateFileName)
}

// loadWindowState 读取状态；缺失、脏 JSON 或尺寸过小一律回落默认值。
func loadWindowState(path string) windowState {
	fallback := windowState{Width: defaultWindowWidth, Height: defaultWindowHeight}
	if path == "" {
		return fallback
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var saved windowState
	if err := json.Unmarshal(data, &saved); err != nil {
		return fallback
	}
	if saved.Width >= minWindowWidth && saved.Height >= minWindowHeight {
		fallback.Width = saved.Width
		fallback.Height = saved.Height
	}
	fallback.X = saved.X
	fallback.Y = saved.Y
	fallback.HasPos = saved.HasPos
	fallback.Maximised = saved.Maximised
	return fallback
}

// writeWindowState 写入无 BOM UTF-8 JSON；目录缺失时自动创建。
func writeWindowState(path string, state windowState) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// visibleOn 判断窗口中心是否落在任一屏幕工作区内；无坐标或无屏幕信息时视为可见。
func (s windowState) visibleOn(areas []windowArea) bool {
	if !s.HasPos || len(areas) == 0 {
		return true
	}
	centerX := s.X + s.Width/2
	centerY := s.Y + s.Height/2
	for _, area := range areas {
		if centerX >= area.X && centerX < area.X+area.Width &&
			centerY >= area.Y && centerY < area.Y+area.Height {
			return true
		}
	}
	return false
}
