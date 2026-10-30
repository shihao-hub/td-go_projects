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

// SessionNewResult represents the result payload of a session/new response.
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
