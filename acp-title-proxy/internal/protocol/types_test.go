package protocol

import (
	"encoding/json"
	"strings"
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
			{"name": "logout", "description": "logout"}
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

	// 原生 2 个 (plan, logout) + 3 个官方思维增强命令 (grill-me, goal, compact) + 1 个技能 = 6
	if len(res.AvailableCommands) != 6 {
		t.Fatalf("expected 6 commands (plan, logout, 3 native augmented, sh-pocock-workflow), got %d", len(res.AvailableCommands))
	}

	names := make([]string, len(res.AvailableCommands))
	for i, c := range res.AvailableCommands {
		names[i] = c.Name
	}
	joined := strings.Join(names, ",")
	for _, expected := range []string{"plan", "logout", "grill-me", "goal", "compact", "sh-pocock-workflow"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing expected command %s in %s", expected, joined)
		}
	}
}

func TestMergeCommandsUpdateNotification(t *testing.T) {
	rawParams := json.RawMessage(`{
		"sessionId": "sess_123",
		"update": {
			"sessionUpdate": "available_commands_update",
			"availableCommands": [
				{"name": "usage", "description": "usage"}
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

	// 原有 1 个(usage) + 3 个官方思维增强命令 + 1 个技能 = 5
	if len(params.Update.AvailableCommands) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(params.Update.AvailableCommands))
	}
}

func TestMergeConfigOptions(t *testing.T) {
	rawResult := json.RawMessage(`{
		"sessionId": "sess_123",
		"configOptions": [
			{"id": "mode", "name": "Mode", "type": "select", "currentValue": "yolo"}
		]
	}`)

	merged, err := MergeConfigOptions(rawResult, DefaultWorkflowConfigOption)
	if err != nil {
		t.Fatal(err)
	}

	var res struct {
		ConfigOptions []ConfigOption `json:"configOptions"`
	}
	if err := json.Unmarshal(merged, &res); err != nil {
		t.Fatal(err)
	}

	if len(res.ConfigOptions) != 2 {
		t.Fatalf("expected 2 configOptions, got %d", len(res.ConfigOptions))
	}
	if res.ConfigOptions[1].ID != "workflow" {
		t.Fatalf("expected second option to be workflow, got %s", res.ConfigOptions[1].ID)
	}
}

func TestReplaceModesInResult(t *testing.T) {
	rawResult := json.RawMessage(`{
		"sessionId": "sess_123",
		"modes": {
			"currentModeId": "yolo",
			"availableModes": [
				{"id": "default", "name": "Default"},
				{"id": "yolo", "name": "YOLO"}
			]
		}
	}`)

	replaced, err := ReplaceModesInResult(rawResult, "plan")
	if err != nil {
		t.Fatal(err)
	}

	var res struct {
		SessionID string       `json:"sessionId"`
		Modes     SessionModes `json:"modes"`
	}
	if err := json.Unmarshal(replaced, &res); err != nil {
		t.Fatal(err)
	}

	if res.SessionID != "sess_123" {
		t.Fatalf("expected sessionId sess_123, got %s", res.SessionID)
	}
	if res.Modes.CurrentModeID != "plan" {
		t.Fatalf("expected currentModeId plan, got %s", res.Modes.CurrentModeID)
	}
	if len(res.Modes.AvailableModes) != 2 {
		t.Fatalf("expected 2 modes (normal, plan), got %d", len(res.Modes.AvailableModes))
	}
	if res.Modes.AvailableModes[0].ID != "normal" || res.Modes.AvailableModes[1].ID != "plan" {
		t.Fatalf("unexpected availableModes: %+v", res.Modes.AvailableModes)
	}
}

