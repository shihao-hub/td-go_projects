// Package cli 实现各子命令与 JSON 包络输出。
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"unicode/utf8"
)

// Pretty 全局开关：--pretty 时缩进 JSON 供人读，默认紧凑单行
var Pretty bool

// Ascii 全局开关：--ascii 时把非 ASCII 字符转义为 \uXXXX，供 PS 5.1 等
// 管道重编码不可靠环境使用（下游 JSON.parse 自动还原），默认关闭
var Ascii bool

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
	b := bytes.TrimRight(buf.Bytes(), "\n")
	// --ascii：非 ASCII 转义。放在 marshal 单点，stdout（writeJSON）与
	// stderr（FailStderr）两个出口同时覆盖
	if Ascii {
		b = escapeNonASCII(b)
	}
	return b, nil
}

// escapeNonASCII 把字节切片中的非 ASCII 字符转义为 \uXXXX（小写十六进制，
// 对齐 Go json 风格；BMP 外字符输出代理对）。
// 安全前提：encoding/json 产物中非 ASCII 字节只出现在字符串字面量内
// （结构字符/缩进/既有转义均 ASCII），字节级后处理安全——只能在 Encode 之后做。
func escapeNonASCII(b []byte) []byte {
	// 快扫：全 ASCII 直接原样返回（绝大多数管理命令纯 ASCII）
	allASCII := true
	for _, c := range b {
		if c >= utf8.RuneSelf {
			allASCII = false
			break
		}
	}
	if allASCII {
		return b
	}

	out := make([]byte, 0, len(b)*2)
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		switch {
		case r < utf8.RuneSelf:
			out = append(out, b[i:i+size]...)
		case r > 0xFFFF:
			// 代理对（Go 内部 rune 直存码点，json 标准库同样拆对输出）
			v := r - 0x10000
			out = append(out, fmt.Sprintf(`\u%04x\u%04x`, 0xD800+(v>>10), 0xDC00+(v&0x3FF))...)
		case r == utf8.RuneError && size == 1:
			// 无效 UTF-8 字节。Encoder 已将其净化为 U+FFFD，理论不可达，防御保留
			out = append(out, "\\ufffd"...)
		default:
			out = append(out, fmt.Sprintf(`\u%04x`, r)...)
		}
		i += size
	}
	return out
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
