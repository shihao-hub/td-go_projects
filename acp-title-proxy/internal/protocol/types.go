package protocol

import (
	"encoding/json"
	"strings"
)

// JSONRPCMessage is a generic JSON-RPC 2.0 message envelope.
type JSONRPCMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   json.RawMessage  `json:"error,omitempty"`
}

// CommandItem represents a slash command definition in ACP.
type CommandItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
}

// SessionNewResult represents the minimal result payload of a session/new response.
type SessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// SessionPromptParams represents the params of a session/prompt request.
type SessionPromptParams struct {
	SessionID string          `json:"sessionId"`
	Prompt    json.RawMessage `json:"prompt"`
}

// ExtractPromptText extracts plain text from the prompt field (supports string or array of content blocks).
func ExtractPromptText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try unmarshaling as plain string
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return strings.TrimSpace(str)
	}

	// Try unmarshaling as array of blocks: [{"type": "text", "text": "..."}]
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

// RewritePrompt replaces the text content inside a raw prompt with newPromptText,
// preserving the original structure (plain string or array of blocks).
func RewritePrompt(raw json.RawMessage, newPromptText string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.Marshal(newPromptText)
	}

	// 1. Try plain string
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return json.Marshal(newPromptText)
	}

	// 2. Try array of generic block objects
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

	// Fallback to plain string if unrecognized structure
	return json.Marshal(newPromptText)
}

// MergeAvailableCommands merges extra commands into rawResult's availableCommands array,
// preserving all existing fields in rawResult and avoiding duplicates.
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

// MergeCommandsUpdateNotification checks if rawParams is an available_commands_update notification,
// and merges extra commands into it if so.
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

// SessionUpdateNotification represents a session/update notification sent to the ACP client (Zed).
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
