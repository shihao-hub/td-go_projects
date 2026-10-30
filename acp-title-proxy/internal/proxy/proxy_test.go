package proxy

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"language_projects/go_projects/acp-title-proxy/internal/protocol"
	"language_projects/go_projects/acp-title-proxy/internal/skill"
)

type mockTitler struct {
	lastPrompt    string
	lastSessionID string
}

func (m *mockTitler) OnSessionNew(sessionID string) {
	m.lastSessionID = sessionID
}

func (m *mockTitler) OnPrompt(sessionID string, prompt string) {
	m.lastSessionID = sessionID
	m.lastPrompt = prompt
}

type mockSkillMgr struct {
	items []protocol.CommandItem
}

func (m *mockSkillMgr) GetCommandItems() []protocol.CommandItem {
	return m.items
}

func (m *mockSkillMgr) MatchAndRewrite(rawPrompt string) (bool, string, string, *skill.Skill) {
	if strings.HasPrefix(rawPrompt, "/my-skill") {
		task := strings.TrimSpace(strings.TrimPrefix(rawPrompt, "/my-skill"))
		rewritten := "AUGMENTED: " + task
		s := &skill.Skill{Name: "my-skill"}
		return true, rewritten, task, s
	}
	return false, rawPrompt, rawPrompt, nil
}

func TestProcessOutboundMergeCommandsAndWorkflowOption(t *testing.T) {
	titler := &mockTitler{}
	skillMgr := &mockSkillMgr{
		items: []protocol.CommandItem{
			{Name: "my-skill", Description: "my skill desc"},
		},
	}
	runner := NewRunner("", nil, titler, skillMgr)

	rawNewResp := []byte(`{"jsonrpc":"2.0","id":1,"result":{"sessionId":"sess_1","availableCommands":[{"name":"plan","description":"plan"}]}}` + "\n")
	out := runner.processOutbound(rawNewResp, nil)

	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(out, &msg); err != nil {
		t.Fatal(err)
	}

	var res struct {
		SessionID         string                  `json:"sessionId"`
		AvailableCommands []protocol.CommandItem  `json:"availableCommands"`
		ConfigOptions     []protocol.ConfigOption `json:"configOptions"`
	}
	if err := json.Unmarshal(msg.Result, &res); err != nil {
		t.Fatal(err)
	}

	if titler.lastSessionID != "sess_1" {
		t.Fatalf("expected titler session_1, got %s", titler.lastSessionID)
	}

	// Native plan + 3 native augmented (grill-me, goal, compact) + 1 custom skill = 5
	if len(res.AvailableCommands) != 5 {
		t.Fatalf("expected 5 commands, got %d: %+v", len(res.AvailableCommands), res.AvailableCommands)
	}

	// Verify modes is replaced with Workflow modes (Normal / Plan)
	var modesRes struct {
		Modes protocol.SessionModes `json:"modes"`
	}
	if err := json.Unmarshal(msg.Result, &modesRes); err != nil {
		t.Fatal(err)
	}
	if modesRes.Modes.CurrentModeID != "normal" {
		t.Fatalf("expected currentModeId normal, got %s", modesRes.Modes.CurrentModeID)
	}
	if len(modesRes.Modes.AvailableModes) != 2 || modesRes.Modes.AvailableModes[1].ID != "plan" {
		t.Fatalf("unexpected availableModes: %+v", modesRes.Modes.AvailableModes)
	}
}

func TestProcessInboundWorkflowPlanHandbrake(t *testing.T) {
	titler := &mockTitler{}
	skillMgr := &mockSkillMgr{}
	runner := NewRunner("", nil, titler, skillMgr)

	var zedStdout bytes.Buffer
	inj := NewInjector(&zedStdout)

	// 1. Zed sets mode = plan (via Shift+Tab / session/set_mode)
	setReq := []byte(`{"jsonrpc":"2.0","id":10,"method":"session/set_mode","params":{"sessionId":"sess_1","modeId":"plan"}}` + "\n")
	outSet := runner.processInbound(setReq, inj)
	if outSet != nil {
		t.Fatal("expected set_mode to be intercepted (returned nil)")
	}

	// Verify empty success result written to Zed
	if !strings.Contains(zedStdout.String(), `"id":10`) || !strings.Contains(zedStdout.String(), `"result":{}`) {
		t.Fatalf("expected success response, got: %s", zedStdout.String())
	}

	// 2. User sends normal prompt in Plan mode -> should inject /plan and PlanModeInstruction
	promptReq1 := []byte(`{"jsonrpc":"2.0","id":11,"method":"session/prompt","params":{"sessionId":"sess_1","prompt":[{"type":"text","text":"设计用户登录模块"}]}}` + "\n")
	outPrompt1 := runner.processInbound(promptReq1, inj)

	var msg1 protocol.JSONRPCMessage
	_ = json.Unmarshal(outPrompt1, &msg1)
	var params1 protocol.SessionPromptParams
	_ = json.Unmarshal(msg1.Params, &params1)
	text1 := protocol.ExtractPromptText(params1.Prompt)

	if !strings.HasPrefix(text1, "/plan ") || !strings.Contains(text1, "【系统约束：当前处于 Plan 规划模式（只读分析阶段）") {
		t.Fatalf("expected /plan and handbrake instruction, got: %s", text1)
	}

	// 3. User urges execution while still in Plan mode -> Handbrake stays on!
	promptReq2 := []byte(`{"jsonrpc":"2.0","id":12,"method":"session/prompt","params":{"sessionId":"sess_1","prompt":[{"type":"text","text":"好的，开始动手！"}]}}` + "\n")
	outPrompt2 := runner.processInbound(promptReq2, inj)

	var msg2 protocol.JSONRPCMessage
	_ = json.Unmarshal(outPrompt2, &msg2)
	var params2 protocol.SessionPromptParams
	_ = json.Unmarshal(msg2.Params, &params2)
	text2 := protocol.ExtractPromptText(params2.Prompt)

	if !strings.Contains(text2, "【系统约束：当前处于 Plan 规划模式（只读分析阶段）") {
		t.Fatalf("handbrake must stay active while in Plan mode, got: %s", text2)
	}

	// 4. User releases handbrake by pressing Shift+Tab (session/set_mode -> normal)
	zedStdout.Reset()
	setNormalReq := []byte(`{"jsonrpc":"2.0","id":13,"method":"session/set_mode","params":{"sessionId":"sess_1","modeId":"normal"}}` + "\n")
	_ = runner.processInbound(setNormalReq, inj)
	if !strings.Contains(zedStdout.String(), `"id":13`) {
		t.Fatalf("expected success response for id 13, got: %s", zedStdout.String())
	}

	// 5. User sends command in Normal mode -> Completely unrestricted!
	promptReq3 := []byte(`{"jsonrpc":"2.0","id":14,"method":"session/prompt","params":{"sessionId":"sess_1","prompt":[{"type":"text","text":"好的，开始动手！"}]}}` + "\n")
	outPrompt3 := runner.processInbound(promptReq3, inj)

	var msg3 protocol.JSONRPCMessage
	_ = json.Unmarshal(outPrompt3, &msg3)
	var params3 protocol.SessionPromptParams
	_ = json.Unmarshal(msg3.Params, &params3)
	text3 := protocol.ExtractPromptText(params3.Prompt)

	if strings.Contains(text3, "【系统约束") || strings.Contains(text3, "/plan") {
		t.Fatalf("normal mode should not have plan constraints, got: %s", text3)
	}
	if text3 != "好的，开始动手！" {
		t.Fatalf("expected original text, got: %s", text3)
	}
}
