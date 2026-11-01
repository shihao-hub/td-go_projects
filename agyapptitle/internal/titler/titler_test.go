package titler

import "testing"

func TestSanitizeTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`"用户认证模块重构"`, "用户认证模块重构"},
		{`“Python脚本调试。”`, "Python脚本调试"},
		{`《Go后台守护进程》！`, "Go后台守护进程"},
		{"  多余 空白 压缩   \n", "多余 空白 压缩"},
		{"这是一个超级超级超级超级长长长长长长长长长长长长长长长长的标题", "这是一个超级超级超级超级长长长长…"},
	}

	for _, tt := range tests {
		got := SanitizeTitle(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeTitle(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}
