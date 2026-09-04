// Package hotkey 提供全局热键：跨平台 Combo 定义与解析在所有平台可用；
// Windows 下通过 RegisterHotKey + 独立线程消息循环实现真实注册。
package hotkey

import (
	"fmt"
	"strings"
)

// Combo 热键组合。Key 取值：A-Z、0-9、F1-F12。
type Combo struct {
	Alt   bool
	Ctrl  bool
	Shift bool
	Key   string
}

// Valid 报告组合是否合法（至少一个修饰键 + 有效主键）。
func (c Combo) Valid() bool {
	if !c.Alt && !c.Ctrl && !c.Shift {
		return false
	}
	_, ok := vkCode(strings.ToUpper(c.Key))
	return ok
}

func (c Combo) String() string {
	var parts []string
	if c.Ctrl {
		parts = append(parts, "Ctrl")
	}
	if c.Alt {
		parts = append(parts, "Alt")
	}
	if c.Shift {
		parts = append(parts, "Shift")
	}
	parts = append(parts, strings.ToUpper(c.Key))
	return strings.Join(parts, "+")
}

// Parse 解析 "Alt+S" / "Ctrl+Shift+T" 形式。
func Parse(s string) (Combo, error) {
	var c Combo
	for _, part := range strings.Split(strings.TrimSpace(s), "+") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "alt":
			c.Alt = true
		case "ctrl", "control":
			c.Ctrl = true
		case "shift":
			c.Shift = true
		default:
			c.Key = strings.ToUpper(strings.TrimSpace(part))
		}
	}
	if !c.Valid() {
		return c, fmt.Errorf("无效热键 %q（需至少一个修饰键 + A-Z/0-9/F1-F12）", s)
	}
	return c, nil
}

// vkCode 返回 Windows 虚拟键码；主键不区分平台，未识别返回 false。
func vkCode(key string) (uint16, bool) {
	switch {
	case len(key) == 1 && key[0] >= 'A' && key[0] <= 'Z':
		return uint16(key[0]), true
	case len(key) == 1 && key[0] >= '0' && key[0] <= '9':
		return uint16(key[0]), true
	case len(key) >= 2 && key[0] == 'F':
		var n int
		if _, err := fmt.Sscanf(key, "F%d", &n); err == nil && n >= 1 && n <= 12 {
			return uint16(0x70 + n - 1), true
		}
	}
	return 0, false
}

// ValidKey 报告主键是否受支持（A-Z / 0-9 / F1-F12），供 UI 捕获组件过滤。
func ValidKey(key string) bool {
	_, ok := vkCode(strings.ToUpper(key))
	return ok
}

// Keys 返回全部可用主键（供捕获组件校验）。
func Keys() []string {
	var ks []string
	for c := 'A'; c <= 'Z'; c++ {
		ks = append(ks, string(c))
	}
	for c := '0'; c <= '9'; c++ {
		ks = append(ks, string(c))
	}
	for i := 1; i <= 12; i++ {
		ks = append(ks, fmt.Sprintf("F%d", i))
	}
	return ks
}
