package model

import "testing"

func TestIsChineseTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"", false},
		{"   ", false},
		{"Refactor User Auth Locks", false},
		{"Fix Python Script", false},
		{"【知识】Douyin Video Summary And Insights", false}, // 混血伪中文（主体纯英文）
		{"[WIP] Implement Go Daemon", false},
		{"用户认证并发锁重构", true},
		{"【待办】用户认证重构", true},
		{"[架构] ConnectRPC 热推送设计", true},
		{"Python 脚本修复", true},
		{"Go 后台守护进程", true},
		{"Debugging", false},
	}

	for _, tt := range tests {
		got := IsChineseTitle(tt.input)
		if got != tt.expected {
			t.Errorf("IsChineseTitle(%q) = %v, expected %v", tt.input, got, tt.expected)
		}
	}
}
