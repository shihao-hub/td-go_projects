// Package cli 实现各子命令与 JSON 包络输出。
package cli

import (
	"bytes"
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
	FailExit(code, msg, 1)
}

// FailExit 输出错误包络到 stdout 并以指定退出码结束。
// start/stop 前置失败（未注册/失效）需保持与 run 一致的 127，但错误走 stdout
// （它们是管理型命令，stdout 无子进程归属问题）。suggestions 同 FailStderr
func FailExit(code, msg string, exitCode int, suggestions ...string) {
	errObj := map[string]any{"code": code, "message": msg}
	if len(suggestions) > 0 {
		errObj["suggestions"] = suggestions
	}
	writeJSON(map[string]any{"ok": false, "error": errObj})
	os.Exit(exitCode)
}

// FailStderr 错误 JSON 走 stderr 并以指定退出码结束（clictl run 前置校验专用，
// 不污染 stdout——run 的 stdout 只属于子进程）。
// suggestions 为可选的相似名提示（未注册工具时），非空时输出 error.suggestions 字段
func FailStderr(code, msg string, exitCode int, suggestions ...string) {
	errObj := map[string]any{"code": code, "message": msg}
	if len(suggestions) > 0 {
		errObj["suggestions"] = suggestions
	}
	b, err := marshal(map[string]any{"ok": false, "error": errObj})
	if err == nil {
		fmt.Fprintln(os.Stderr, string(b))
	}
	os.Exit(exitCode)
}

func marshal(payload any) ([]byte, error) {
	// 默认 Marshal 会把 < > & 转义成 \u003c 等（防 HTML XSS），
	// CLI 输出无此需求，用 Encoder 关闭 HTML 转义
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if Pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(payload); err != nil {
		return nil, err
	}
	// Encode 自带换行，writeJSON 里 Fprintln 会再加一层，这里去掉
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
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
