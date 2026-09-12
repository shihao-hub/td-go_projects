package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// version 版本号：默认 dev，发布构建时经 -ldflags "-X main.version=X.Y.Z" 注入
var version = "dev"

// protocolVersion 输出协议版本：stdout JSON schema 发生 breaking 变更时 bump
const protocolVersion = 1

// Pretty 全局开关：--pretty 时 JSON 缩进输出（人读）
var Pretty bool

// stdoutWriter JSON 输出目标，测试中可注入 bytes.Buffer
var stdoutWriter io.Writer = os.Stdout

// Emit 成功包络 {"ok":true,"data":...}
func Emit(data any) {
	writeJSON(map[string]any{"ok": true, "data": data})
}

// Fail 失败包络 {"ok":false,"error":{"code","message"}}，返回退出码 1
// （不直接 os.Exit，保证 run 可被测试驱动）
func Fail(code, msg string) int {
	writeJSON(map[string]any{
		"ok":    false,
		"error": map[string]string{"code": code, "message": msg},
	})
	return exitError
}

func marshal(payload any) ([]byte, error) {
	if Pretty {
		return json.MarshalIndent(payload, "", "  ")
	}
	return json.Marshal(payload)
}

// writeJSON 唯一输出出口，禁止业务代码直接 fmt.Println 到 stdout
func writeJSON(payload map[string]any) {
	b, err := marshal(payload)
	if err != nil {
		// 序列化失败兜底：手写最小错误 JSON，绝不 panic
		fmt.Fprintln(stdoutWriter, `{"ok":false,"error":{"code":"internal","message":"JSON 序列化失败"}}`)
		return
	}
	fmt.Fprintln(stdoutWriter, string(b))
}
