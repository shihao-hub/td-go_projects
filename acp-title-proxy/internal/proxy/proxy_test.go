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

	// Verify Workflow config option is injected
	foundWorkflow := false
	for _, opt := range res.ConfigOptions {
		if opt.ID == "workflow" {
			foundWorkflow = true
			if len(opt.Options) != 2 || opt.Options[1].Value != "plan" {
				t.Fatalf("unexpected workflow options: %+v", opt.Options)
			}
		}
	}
	if !foundWorkflow {
		t.Fatal("workflow config option not injected into result")
	}
}

func TestProcessInboundWorkflowPlanLifecycle(t *testing.T) {
	titler := &mockTitler{}
	skillMgr := &mockSkillMgr{}
	runner := NewRunner("", nil, titler, skillMgr)

	var zedStdout bytes.Buffer
	inj := NewInjector(&zedStdout)

	// 1. Zed sets workflow = plan
	setReq := []byte(`{"jsonrpc":"2.0","id":10,"method":"session/set_config_option","params":{"sessionId":"sess_1","configId":"workflow","value":"plan"}}` + "\n")
	outSet := runner.processInbound(setReq, inj)
	if outSet != nil {
		t.Fatal("expected set_config_option to be intercepted (returned nil)")
	}

	// Verify response written to Zed
	if !strings.Contains(zedStdout.String(), `"id":10`) {
		t.Fatalf("expected response for id 10, got: %s", zedStdout.String())
	}

	// 2. User sends normal prompt in Plan mode -> should auto-inject /plan
	promptReq1 := []byte(`{"jsonrpc":"2.0","id":11,"method":"session/prompt","params":{"sessionId":"sess_1","prompt":[{"type":"text","text":"设计用户登录模块"}]}}` + "\n")
	outPrompt1 := runner.processInbound(promptReq1, inj)

	var msg1 protocol.JSONRPCMessage
	_ = json.Unmarshal(outPrompt1, &msg1)
	var params1 protocol.SessionPromptParams
	_ = json.Unmarshal(msg1.Params, &params1)
	text1 := protocol.ExtractPromptText(params1.Prompt)

	if !strings.HasPrefix(text1, "/plan 设计用户登录模块") {
		t.Fatalf("expected /plan injection, got: %s", text1)
	}

	// 3. Child responds to prompt 11 (plan generated and finished streaming)
	planResp := []byte(`{"jsonrpc":"2.0","id":11,"result":{}}` + "\n")
	runner.processOutbound(planResp, inj)

	// 4. User replies with approval ("好的，开始动手！") -> should NOT inject /plan!
	promptReq2 := []byte(`{"jsonrpc":"2.0","id":12,"method":"session/prompt","params":{"sessionId":"sess_1","prompt":[{"type":"text","text":"好的，开始动手！"}]}}` + "\n")
	outPrompt2 := runner.processInbound(promptReq2, inj)

	var msg2 protocol.JSONRPCMessage
	_ = json.Unmarshal(outPrompt2, &msg2)
	var params2 protocol.SessionPromptParams
	_ = json.Unmarshal(msg2.Params, &params2)
	text2 := protocol.ExtractPromptText(params2.Prompt)

	if strings.Contains(text2, "/plan") {
		t.Fatalf("approval message should NOT have /plan, got: %s", text2)
	}
	if text2 != "好的，开始动手！" {
		t.Fatalf("expected raw approval text, got: %s", text2)
	}
}
