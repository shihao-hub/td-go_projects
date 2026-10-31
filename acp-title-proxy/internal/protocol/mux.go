package protocol

// 本文件提供 ACP 多路复用（mux）所需的 JSON-RPC 行协议工具。
// 与 types.go 中的旧类型并存，互不影响。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Message 是 ACP 使用的 JSON-RPC 2.0 消息信封。
// raw 保存原始行，用于无损转发（通知/响应不重新序列化）。
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`

	raw []byte
}

func ParseMessage(line []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(line, &m); err != nil {
		return Message{}, err
	}
	m.raw = append([]byte(nil), line...)
	return m, nil
}

func (m Message) Raw() []byte { return m.raw }

func (m Message) HasID() bool {
	return len(m.ID) > 0 && string(m.ID) != "null"
}

func (m Message) IsRequest() bool {
	return m.Method != "" && m.HasID()
}

func (m Message) IsNotification() bool {
	return m.Method != "" && !m.HasID()
}

func (m Message) IsResponse() bool {
	return m.Method == "" && m.HasID()
}

// SessionID 从 params 中读取 sessionId；不存在时返回空串。
func (m Message) SessionID() string {
	if len(m.Params) == 0 {
		return ""
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(m.Params, &p)
	return p.SessionID
}

// ExtractSessionID 从 session/new、session/load 等响应结果中读取 sessionId。
func ExtractSessionID(result json.RawMessage) string {
	if len(result) == 0 {
		return ""
	}
	var r struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(result, &r)
	return r.SessionID
}

// CapabilitiesHash 对 initialize 的 clientCapabilities 做规范化哈希。
func CapabilitiesHash(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var p struct {
		ClientCapabilities json.RawMessage `json:"clientCapabilities"`
	}
	_ = json.Unmarshal(params, &p)
	if len(p.ClientCapabilities) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(p.ClientCapabilities, &v); err != nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// WithID 返回替换了 id 的消息；原始行缓存被清除，确保重新序列化。
func WithID(m Message, id json.RawMessage) Message {
	m.ID = id
	m.raw = nil
	return m
}

// StripRaw 丢弃原始行缓存，使修改过 Params/Result 的消息重新序列化。
func StripRaw(m Message) Message {
	m.raw = nil
	return m
}

// MarshalMessage 优先返回原始行（无损），否则重新序列化。
func MarshalMessage(m Message) []byte {
	if len(m.raw) > 0 {
		return m.raw
	}
	b, _ := json.Marshal(m)
	return b
}

func ErrorResponse(id json.RawMessage, code int, message string) []byte {
	m := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	}
	b, _ := json.Marshal(m)
	return b
}

func ResultResponse(id json.RawMessage, result json.RawMessage) []byte {
	m := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	b, _ := json.Marshal(m)
	return b
}

func Notification(method string, params any) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	return b
}

func QuoteID(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// IDString 将任意 JSON-RPC id 规范成字符串，用于 map 键。
func IDString(id json.RawMessage) string {
	if len(id) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(id, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(id, &n); err == nil {
		return n.String()
	}
	return string(id)
}
