// Package cli 的输出纪律（参考 docs/go_projects/clictl/Go CLI JSON 输出模式参考.md）：
// stdout 永远输出合法 JSON，人读体验交给 --pretty。
// 所有输出必须走本文件的 Emit/Fail/FailStderr，业务代码禁止直接 fmt.Println。
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
	writeJSON(map[string]any{"ok": false, "error": map[string]string{"code": code, "message": msg}})
	os.Exit(1)
}

// FailStderr 错误 JSON 走 stderr 并以指定退出码结束（stdout 归属其他内容时用，
// 不污染 stdout）
func FailStderr(code, msg string, exitCode int) {
	b, err := marshal(map[string]any{"ok": false, "error": map[string]string{"code": code, "message": msg}})
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

// writeJSON 唯一出口：序列化失败兜底手写最小错误 JSON，绝不 panic
func writeJSON(payload map[string]any) {
	b, err := marshal(payload)
	if err != nil {
		fmt.Println(`{"ok":false,"error":{"code":"internal","message":"JSON 序列化失败"}}`)
		os.Exit(1)
	}
	fmt.Println(string(b))
}
