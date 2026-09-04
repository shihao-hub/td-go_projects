//go:build !windows

package hotkey

import "fmt"

// Manager 非 Windows 平台为空实现（本工具仅面向 Windows）。
type Manager struct{}

func NewManager() *Manager { return &Manager{} }

func (m *Manager) Set(combo Combo, cb func()) error {
	return fmt.Errorf("hotkey: unsupported on this platform")
}

func (m *Manager) Clear() {}
