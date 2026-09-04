// Package protocol 定义 aiquick UI(aiquick.exe) 与后端(aiquickd.exe)之间
// 的行式 JSON(NDJSON) 通信协议的线上消息结构。
//
// 约定：
//   - 每条消息为一行紧凑 JSON，以 \n 结尾；
//   - 前端 -> 后端：Request；
//   - 后端 -> 前端：Response(应答，id 对应请求) 或 Event(主动推送)；
//   - Event 携带 rid 时表示关联到该 id 的请求（流式输出场景）；
//   - 后端 stdout 只允许出现本协议消息，日志一律走 stderr。
package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Version 协议版本，hello 握手时校验。
const Version = 1

// 内置方法名。
const (
	MethodHello    = "hello"
	MethodPing     = "ping"
	MethodShutdown = "shutdown"
)

// 常用错误码。
const (
	CodeBadRequest = "ERR_BAD_REQUEST"
	CodeNotFound   = "ERR_NOT_FOUND"
	CodeInternal   = "ERR_INTERNAL"
	CodeUpstream   = "ERR_UPSTREAM" // LLM 上游错误
	CodeCancelled  = "ERR_CANCELLED"
)

// Request 前端发往后端的调用请求。
type Request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response 后端对 Request 的应答。OK 为 true 时 Result 有效，否则 Error 有效。
type Response struct {
	ID     int64           `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error 协议层错误。
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Event 后端主动推送的消息。RID 非 0 时关联到对应请求（流式 chunk）。
type Event struct {
	Event string          `json:"event"`
	RID   int64           `json:"rid,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Envelope 通用解码信封：一行 JSON 要么是 Response（有 ok 字段），
// 要么是 Event（有 event 字段），用 IsEvent 区分。
type Envelope struct {
	ID     int64           `json:"id"`
	OK     *bool           `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
	Event  string          `json:"event"`
	RID    int64           `json:"rid"`
	Data   json.RawMessage `json:"data"`
}

// DecodeEnvelope 解码一行协议消息为信封。
func DecodeEnvelope(line []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(bytes.TrimSpace(line), &e); err != nil {
		return e, fmt.Errorf("decode line: %w", err)
	}
	return e, nil
}

// IsEvent 报告该信封是否为事件推送。
func (e Envelope) IsEvent() bool { return e.Event != "" }

// EncodeLine 将消息序列化为一行 JSON（自带 \n）。
func EncodeLine(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode line: %w", err)
	}
	return buf.Bytes(), nil
}
