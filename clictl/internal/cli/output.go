// Package cli 实现各子命令与 JSON 包络输出。
package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

// Pretty 全局开关：--pretty 时缩进 JSON 供人读，默认紧凑单行
var Pretty bool

// Emit 输出成功包络 {"ok":true,"data":...} 到 stdout
func Emit(data any) {
	writeJSON(map[string]any{"ok": true, "data": data})
}

// Fail 输出错误包络到 stdout 并以退出码 1 结束（管理命令错误也走 stdout，
// stdout 永远是合法 JSON）
func Fail(code, msg string) {
	writeJSON(map[string]any{"ok": false, "error": map[string]string{"code": code, "message": msg}})
	os.Exit(1)
}

// FailStderr 错误 JSON 走 stderr 并以指定退出码结束（clictl run 前置校验专用，
// 不污染 stdout——run 的 stdout 只属于子进程）
func FailStderr(code, msg string, exitCode int) {
	b, err := marshal(map[string]any{"ok": false, "error": map[string]string{"code": code, "message": msg}})
	if err == nil {
		fmt.Fprintln(os.Stderr, string(b))
	}
	os.Exit(exitCode)
}

func marshal(payload any) ([]byte, error) {
	if Pretty {
		return json.MarshalIndent(payload, "", "  ")
	}
	return json.Marshal(payload)
}

func writeJSON(payload map[string]any) {
	b, err := marshal(payload)
	if err != nil {
		// 序列化失败兜底：手写最小错误 JSON，避免 panic
		fmt.Println(`{"ok":false,"error":{"code":"internal","message":"JSON 序列化失败"}}`)
		os.Exit(1)
	}
	fmt.Println(string(b))
}
