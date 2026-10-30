package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/protocol"
	"language_projects/go_projects/acp-title-proxy/internal/skill"
)

// duplexPipe creates a bidirectional in-memory ReadWriteCloser pair.
type duplexPipe struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (d *duplexPipe) Read(p []byte) (n int, err error) {
	return d.reader.Read(p)
}

func (d *duplexPipe) Write(p []byte) (n int, err error) {
	return d.writer.Write(p)
}

func (d *duplexPipe) Close() error {
	_ = d.reader.Close()
	return d.writer.Close()
}

func TestE2E_ProxySkillAugmentation(t *testing.T) {
	// Initialize real skill manager with default dirs
	cfg := &config.SkillsConfig{
		Enabled:          true,
		Prefix:           "skill:",
		Dirs:             config.DefaultSkillDirs(),
		AutoInjectPrompt: true,
	}
	skillMgr := skill.NewManager(cfg)
	commands := skillMgr.GetCommandItems()
	if len(commands) == 0 {
		t.Fatal("expected at least 1 skill discovered")
	}

	// Create duplex stream simulating Child (e.g. AGY server)
	childToProxyR, childToProxyW := io.Pipe()
	proxyToChildR, proxyToChildW := io.Pipe()

	proxyChildStream := &duplexPipe{reader: childToProxyR, writer: proxyToChildW}
	mockChildStream := &duplexPipe{reader: proxyToChildR, writer: childToProxyW}

	// Create injector writing to simulated Zed Stdout
	var zedStdout bytes.Buffer
	inj := NewInjector(&zedStdout)

	zedStdinR, zedStdinW := io.Pipe()
	defer zedStdinR.Close()
	defer zedStdinW.Close()

	runner := NewRunner("", nil, nil, skillMgr)
	runner.Stdin = zedStdinR

	// Run stream in background
	go func() {
		_ = runner.RunStream(proxyChildStream, inj)
	}()

	// 1. Child sends session/new response (simulating native agy response with 3 commands)
	nativeNewResp := protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      rawID(1),
		Result: json.RawMessage(`{
			"sessionId": "sess_test_123",
			"availableCommands": [
				{"name": "plan", "description": "Plan carefully"},
				{"name": "logout", "description": "Logout"},
				{"name": "usage", "description": "Usage limits"}
			]
		}`),
	}
	respBytes, _ := json.Marshal(nativeNewResp)
	respBytes = append(respBytes, '\n')
	_, _ = mockChildStream.Write(respBytes)

	// Wait for Zed to receive augmented response
	time.Sleep(100 * time.Millisecond)

	zedLines := strings.Split(strings.TrimSpace(zedStdout.String()), "\n")
	if len(zedLines) == 0 || zedLines[0] == "" {
		t.Fatal("Zed stdout is empty")
	}

	var zedNewResp protocol.JSONRPCMessage
	if err := json.Unmarshal([]byte(zedLines[0]), &zedNewResp); err != nil {
		t.Fatalf("failed to parse zed response: %v", err)
	}

	var zedResult struct {
		SessionID         string                 `json:"sessionId"`
		AvailableCommands []protocol.CommandItem `json:"availableCommands"`
	}
	if err := json.Unmarshal(zedNewResp.Result, &zedResult); err != nil {
		t.Fatalf("failed to parse zed availableCommands: %v", err)
	}

	if zedResult.SessionID != "sess_test_123" {
		t.Fatalf("unexpected sessionId: %s", zedResult.SessionID)
	}

	// Must have native 3 commands + generic skill + discovered skills
	if len(zedResult.AvailableCommands) < 4 {
		t.Fatalf("expected >= 4 commands, got %d", len(zedResult.AvailableCommands))
	}

	foundPlan := false
	foundSkill := false
	for _, c := range zedResult.AvailableCommands {
		if c.Name == "plan" {
			foundPlan = true
		}
		if c.Name == "skill" {
			foundSkill = true
		}
	}
	if !foundPlan {
		t.Fatal("native 'plan' command lost!")
	}
	if !foundSkill {
		t.Fatal("generic 'skill' command not injected!")
	}

	// 2. Test Inbound: User invokes a skill via prefixed /skill:name
	userPromptPrefixed := []byte(`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"sess_test_123","prompt":"/skill:sh-pocock-workflow 帮我审查架构"}}` + "\n")
	processedPrefixed := runner.processInbound(userPromptPrefixed)

	var inboundPrefixed protocol.JSONRPCMessage
	if err := json.Unmarshal(processedPrefixed, &inboundPrefixed); err != nil {
		t.Fatalf("failed to unmarshal processed inbound: %v", err)
	}
	var paramsPrefixed protocol.SessionPromptParams
	_ = json.Unmarshal(inboundPrefixed.Params, &paramsPrefixed)
	extractedPrefixed := protocol.ExtractPromptText(paramsPrefixed.Prompt)
	if !strings.Contains(extractedPrefixed, "【系统注入：激活技能 sh-pocock-workflow】") {
		t.Fatalf("expected prompt injection for /skill:sh-pocock-workflow, got:\n%s", extractedPrefixed)
	}

	// 2b. Test Inbound: User invokes a skill via generic /skill name
	userPromptReq := []byte(`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"sess_test_123","prompt":"/skill sh-pocock-workflow 帮我审查架构"}}` + "\n")
	processedInbound := runner.processInbound(userPromptReq)

	var inboundMsg protocol.JSONRPCMessage
	if err := json.Unmarshal(processedInbound, &inboundMsg); err != nil {
		t.Fatalf("failed to unmarshal processed inbound: %v", err)
	}

	var promptParams protocol.SessionPromptParams
	if err := json.Unmarshal(inboundMsg.Params, &promptParams); err != nil {
		t.Fatalf("failed to unmarshal prompt params: %v", err)
	}

	extractedPrompt := protocol.ExtractPromptText(promptParams.Prompt)
	if !strings.Contains(extractedPrompt, "【系统注入：激活技能 sh-pocock-workflow】") {
		t.Fatalf("expected prompt injection, got:\n%s", extractedPrompt)
	}
	if !strings.Contains(extractedPrompt, "帮我审查架构") {
		t.Fatalf("expected user task preserved, got:\n%s", extractedPrompt)
	}
	if !strings.Contains(extractedPrompt, "SKILL.md") {
		t.Fatalf("expected SKILL.md path in prompt, got:\n%s", extractedPrompt)
	}

	// 3. Test Inbound: User invokes native /plan command
	planReq := []byte(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"sess_test_123","prompt":"/plan 仔细规划"}}` + "\n")
	processedPlan := runner.processInbound(planReq)

	var planMsg protocol.JSONRPCMessage
	if err := json.Unmarshal(processedPlan, &planMsg); err != nil {
		t.Fatalf("failed to unmarshal processed plan: %v", err)
	}
	var planParams protocol.SessionPromptParams
	_ = json.Unmarshal(planMsg.Params, &planParams)
	if protocol.ExtractPromptText(planParams.Prompt) != "/plan 仔细规划" {
		t.Fatalf("native /plan was corrupted! got: %s", protocol.ExtractPromptText(planParams.Prompt))
	}

	_ = proxyChildStream.Close()
	_ = mockChildStream.Close()
}

func rawID(id int) *json.RawMessage {
	b, _ := json.Marshal(id)
	rm := json.RawMessage(b)
	return &rm
}
