package proxy

import (
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

type mockTracker struct {
	promptStarts int
	promptDones  int
	lastSession  string
}

func (m *mockTracker) OnPromptStart()          { m.promptStarts++ }
func (m *mockTracker) OnPromptDone()           { m.promptDones++ }
func (m *mockTracker) OnSessionID(sid string)  { m.lastSession = sid }

func TestProcessOutboundMergeCommands(t *testing.T) {
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
		SessionID         string                 `json:"sessionId"`
		AvailableCommands []protocol.CommandItem `json:"availableCommands"`
	}
	if err := json.Unmarshal(msg.Result, &res); err != nil {
		t.Fatal(err)
	}

	if titler.lastSessionID != "sess_1" {
		t.Fatalf("expected titler session_1, got %s", titler.lastSessionID)
	}

	if len(res.AvailableCommands) != 5 {
		t.Fatalf("expected 5 commands (plan, grill-me, goal, compact, my-skill), got %d", len(res.AvailableCommands))
	}
	cmdMap := make(map[string]bool)
	for _, c := range res.AvailableCommands {
		cmdMap[c.Name] = true
	}
	for _, expected := range []string{"plan", "grill-me", "goal", "compact", "my-skill"} {
		if !cmdMap[expected] {
			t.Fatalf("missing expected command: %s", expected)
		}
	}
}

func TestProcessInboundRewritePrompt(t *testing.T) {
	titler := &mockTitler{}
	skillMgr := &mockSkillMgr{}
	runner := NewRunner("", nil, titler, skillMgr)

	rawPromptReq := []byte(`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"sess_1","prompt":[{"type":"text","text":"/my-skill do this work"}]}}` + "\n")
	out := runner.processInbound(rawPromptReq)

	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(out, &msg); err != nil {
		t.Fatal(err)
	}

	var params protocol.SessionPromptParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		t.Fatal(err)
	}

	extractedText := protocol.ExtractPromptText(params.Prompt)
	if extractedText != "AUGMENTED: do this work" {
		t.Fatalf("expected augmented prompt, got: %s", extractedText)
	}

	// 验证传给 titler 的是原始任务，而非增强后的文本！
	if titler.lastPrompt != "do this work" {
		t.Fatalf("expected titler to receive clean task 'do this work', got '%s'", titler.lastPrompt)
	}
}

func TestStateTrackerLifecycle(t *testing.T) {
	tracker := &mockTracker{}
	runner := NewRunner("", nil, nil, nil)
	runner.Tracker = tracker

	// 1. session/new 响应
	rawNew := []byte(`{"jsonrpc":"2.0","id":1,"result":{"sessionId":"sess_abc"}}` + "\n")
	runner.processOutbound(rawNew, nil)
	if tracker.lastSession != "sess_abc" {
		t.Fatalf("expected tracker session sess_abc, got %s", tracker.lastSession)
	}

	// 2. session/prompt 请求 -> Tracker.OnPromptStart
	rawPrompt := []byte(`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"sess_abc","prompt":"hi"}}` + "\n")
	runner.processInbound(rawPrompt)
	if tracker.promptStarts != 1 {
		t.Fatalf("expected 1 promptStart, got %d", tracker.promptStarts)
	}

	// 3. session/prompt 响应 -> Tracker.OnPromptDone
	rawResp := []byte(`{"jsonrpc":"2.0","id":2,"result":{"stopReason":"end_turn"}}` + "\n")
	runner.processOutbound(rawResp, nil)
	if tracker.promptDones != 1 {
		t.Fatalf("expected 1 promptDone, got %d", tracker.promptDones)
	}
}
