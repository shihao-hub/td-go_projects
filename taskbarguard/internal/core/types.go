package core

import "fmt"

// 项目级的稳定标识，供 CLI schema、计划任务与 GUI 元信息共用。
const (
	// ToolName 工具名，同时作为数据目录名。
	ToolName = "taskbarguard"

	// SchemaVersion CLI 契约版本。
	SchemaVersion = "1.0.0"

	// ScheduledTask 登录守护计划任务名。
	ScheduledTask = "TaskbarIconGuard"

	// DefaultScriptsDir 脚本资产目录名（相对可执行文件或工作目录）。
	DefaultScriptsDir = "scripts"
)

// AppInfo 一个可调度的目标应用。
type AppInfo struct {
	Name   string   `json:"name"`
	Styles []string `json:"styles"`
	Script string   `json:"script"`
}

// RunOptions run 命令的可选参数。
type RunOptions struct {
	Style           string
	CustomIcon      string
	Kill            bool
	RestartExplorer bool
}

// ApplyOptions apply 命令的可选参数。
type ApplyOptions struct {
	App             string
	Kill            bool
	RestartExplorer bool
}

// RevertOptions revert 命令的可选参数。
type RevertOptions struct {
	RestartExplorer bool
}

// Error 带机器可读 code 的业务错误，CLI 与 HTTP 层共用同一套映射。
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

func newError(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ErrorCode 从 error 中提取业务错误码；非业务错误统一归为 internal_error。
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if business, ok := err.(*Error); ok {
		return business.Code
	}
	return "internal_error"
}
