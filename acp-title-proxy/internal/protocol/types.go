package protocol

import (
	"encoding/json"
	"strings"
)

// JSONRPCMessage 是通用的 JSON-RPC 2.0 消息信封。
type JSONRPCMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   json.RawMessage  `json:"error,omitempty"`
}

// CommandItem 表示 ACP 中的斜杠命令定义。
type CommandItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
}

// SessionNewResult 表示 session/new 响应的最小结果载荷。
type SessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// SessionPromptParams 表示 session/prompt 请求的 params。
type SessionPromptParams struct {
	SessionID string          `json:"sessionId"`
	Prompt    json.RawMessage `json:"prompt"`
}

// ExtractPromptText 从 prompt 字段提取纯文本（支持字符串或内容块数组）。
func ExtractPromptText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// 尝试按普通字符串解析
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return strings.TrimSpace(str)
	}

	// 尝试按内容块数组解析：[{"type": "text", "text": "..."}]
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Text != "" {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(b.Text)
			}
		}
		return strings.TrimSpace(sb.String())
	}

	return ""
}

// RewritePrompt 将原始 prompt 中的文本内容替换为 newPromptText，
// 并保持原有结构（普通字符串或内容块数组）。
func RewritePrompt(raw json.RawMessage, newPromptText string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.Marshal(newPromptText)
	}

	// 1. 尝试普通字符串
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return json.Marshal(newPromptText)
	}

	// 2. 尝试通用内容块对象数组
	var blocks []map[string]interface{}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		replaced := false
		for _, b := range blocks {
			if t, ok := b["type"].(string); ok && t == "text" {
				b["text"] = newPromptText
				replaced = true
				break
			}
		}
		if !replaced {
			blocks = append([]map[string]interface{}{
				{"type": "text", "text": newPromptText},
			}, blocks...)
		}
		return json.Marshal(blocks)
	}

	// 结构无法识别时回退为普通字符串
	return json.Marshal(newPromptText)
}

// MergeAvailableCommands 将额外命令合并进 rawResult 的 availableCommands 数组，
// 保留 rawResult 中的全部既有字段并避免重复。
func MergeAvailableCommands(rawResult json.RawMessage, extra []CommandItem) (json.RawMessage, error) {
	if len(rawResult) == 0 || len(extra) == 0 {
		return rawResult, nil
	}

	var resultMap map[string]json.RawMessage
	if err := json.Unmarshal(rawResult, &resultMap); err != nil {
		return rawResult, err
	}

	var existing []CommandItem
	if rawCmds, ok := resultMap["availableCommands"]; ok && len(rawCmds) > 0 {
		_ = json.Unmarshal(rawCmds, &existing)
	}

	seen := make(map[string]bool)
	var merged []CommandItem

	for _, c := range existing {
		lower := strings.ToLower(c.Name)
		if !seen[lower] {
			seen[lower] = true
			merged = append(merged, c)
		}
	}

	for _, c := range extra {
		lower := strings.ToLower(c.Name)
		if !seen[lower] {
			seen[lower] = true
			merged = append(merged, c)
		}
	}

	mergedBytes, err := json.Marshal(merged)
	if err != nil {
		return rawResult, err
	}

	resultMap["availableCommands"] = mergedBytes
	return json.Marshal(resultMap)
}

// MergeCommandsUpdateNotification 判断 rawParams 是否为 available_commands_update 通知，
// 若是则将额外命令合并进去。
func MergeCommandsUpdateNotification(rawParams json.RawMessage, extra []CommandItem) (json.RawMessage, bool, error) {
	if len(rawParams) == 0 || len(extra) == 0 {
		return rawParams, false, nil
	}

	var paramsMap map[string]json.RawMessage
	if err := json.Unmarshal(rawParams, &paramsMap); err != nil {
		return rawParams, false, err
	}

	updateRaw, ok := paramsMap["update"]
	if !ok || len(updateRaw) == 0 {
		return rawParams, false, nil
	}

	var updateMap map[string]json.RawMessage
	if err := json.Unmarshal(updateRaw, &updateMap); err != nil {
		return rawParams, false, err
	}

	var sessionUpdate string
	if rawSU, ok := updateMap["sessionUpdate"]; ok {
		_ = json.Unmarshal(rawSU, &sessionUpdate)
	}

	if sessionUpdate != "available_commands_update" {
		return rawParams, false, nil
	}

	var existing []CommandItem
	if rawCmds, ok := updateMap["availableCommands"]; ok && len(rawCmds) > 0 {
		_ = json.Unmarshal(rawCmds, &existing)
	}

	seen := make(map[string]bool)
	var merged []CommandItem

	for _, c := range existing {
		lower := strings.ToLower(c.Name)
		if !seen[lower] {
			seen[lower] = true
			merged = append(merged, c)
		}
	}

	for _, c := range extra {
		lower := strings.ToLower(c.Name)
		if !seen[lower] {
			seen[lower] = true
			merged = append(merged, c)
		}
	}

	mergedBytes, err := json.Marshal(merged)
	if err != nil {
		return rawParams, false, err
	}

	updateMap["availableCommands"] = mergedBytes
	newUpdateRaw, err := json.Marshal(updateMap)
	if err != nil {
		return rawParams, false, err
	}

	paramsMap["update"] = newUpdateRaw
	newParamsRaw, err := json.Marshal(paramsMap)
	if err != nil {
		return rawParams, false, err
	}

	return newParamsRaw, true, nil
}

// SessionUpdateNotification 表示发送给 ACP 客户端（Zed）的 session/update 通知。
type SessionUpdateNotification struct {
	JSONRPC string              `json:"jsonrpc"`
	Method  string              `json:"method"`
	Params  SessionUpdateParams `json:"params"`
}

type SessionUpdateParams struct {
	SessionID string            `json:"sessionId"`
	Update    SessionInfoUpdate `json:"update"`
}

type SessionInfoUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`
	Title         string `json:"title"`
}
