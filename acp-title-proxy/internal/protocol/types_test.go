package protocol

import (
	"encoding/json"
	"testing"
)

func TestRewritePromptString(t *testing.T) {
	raw := json.RawMessage(`"/sh-pocock-workflow test task"`)
	rewritten, err := RewritePrompt(raw, "augmented text")
	if err != nil {
		t.Fatal(err)
	}

	var str string
	if err := json.Unmarshal(rewritten, &str); err != nil {
		t.Fatal(err)
	}
	if str != "augmented text" {
		t.Fatalf("expected 'augmented text', got '%s'", str)
	}
}

func TestRewritePromptBlocks(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"/sh-pocock-workflow test task"},{"type":"other","data":"keep"}]`)
	rewritten, err := RewritePrompt(raw, "augmented text")
	if err != nil {
		t.Fatal(err)
	}

	var blocks []map[string]interface{}
	if err := json.Unmarshal(rewritten, &blocks); err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0]["text"] != "augmented text" {
		t.Fatalf("expected block[0].text to be 'augmented text', got '%v'", blocks[0]["text"])
	}
	if blocks[1]["data"] != "keep" {
		t.Fatalf("expected block[1] to be preserved, got '%v'", blocks[1])
	}
}

func TestMergeAvailableCommands(t *testing.T) {
	rawResult := json.RawMessage(`{
		"sessionId": "sess_123",
		"availableCommands": [
			{"name": "plan", "description": "plan task"},
			{"name": "logout", "description": "logout"},
			{"name": "skill", "description": "generic skill placeholder"}
		],
		"customField": 42
	}`)

	extra := []CommandItem{
		{Name: "sh-pocock-workflow", Description: "workflow skill"},
		{Name: "plan", Description: "should not duplicate"},
	}

	merged, err := MergeAvailableCommands(rawResult, extra)
	if err != nil {
		t.Fatal(err)
	}

	var res struct {
		SessionID         string        `json:"sessionId"`
		AvailableCommands []CommandItem `json:"availableCommands"`
		CustomField       int           `json:"customField"`
	}
	if err := json.Unmarshal(merged, &res); err != nil {
		t.Fatal(err)
	}

	if res.SessionID != "sess_123" || res.CustomField != 42 {
		t.Fatalf("fields not preserved: %+v", res)
	}

	// 原生 2 (plan, logout; skill 排除) + 官方思维 3 (grill-me, goal, compact) + 自定义 1 (sh-pocock-workflow) = 6
	if len(res.AvailableCommands) != 6 {
		t.Fatalf("expected 6 commands, got %d", len(res.AvailableCommands))
	}

	cmdMap := make(map[string]bool)
	for _, c := range res.AvailableCommands {
		cmdMap[c.Name] = true
	}

	if cmdMap["skill"] {
		t.Fatal("unexpected generic 'skill' command found in list")
	}
	for _, expected := range []string{"plan", "logout", "grill-me", "goal", "compact", "sh-pocock-workflow"} {
		if !cmdMap[expected] {
			t.Fatalf("missing expected command: %s", expected)
		}
	}
}

func TestMergeCommandsUpdateNotification(t *testing.T) {
	rawParams := json.RawMessage(`{
		"sessionId": "sess_123",
		"update": {
			"sessionUpdate": "available_commands_update",
			"availableCommands": [
				{"name": "usage", "description": "usage"},
				{"name": "skill", "description": "generic skill placeholder"}
			]
		}
	}`)

	extra := []CommandItem{
		{Name: "anatomy", Description: "isometric diagrams"},
	}

	newParams, matched, err := MergeCommandsUpdateNotification(rawParams, extra)
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("expected matched to be true")
	}

	var params struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate     string        `json:"sessionUpdate"`
			AvailableCommands []CommandItem `json:"availableCommands"`
		} `json:"update"`
	}
	if err := json.Unmarshal(newParams, &params); err != nil {
		t.Fatal(err)
	}

	cmdMap := make(map[string]bool)
	for _, c := range params.Update.AvailableCommands {
		cmdMap[c.Name] = true
	}

	if cmdMap["skill"] {
		t.Fatal("generic 'skill' placeholder should have been removed")
	}
	for _, expected := range []string{"usage", "grill-me", "goal", "compact", "anatomy"} {
		if !cmdMap[expected] {
			t.Fatalf("missing expected command: %s", expected)
		}
	}
}
