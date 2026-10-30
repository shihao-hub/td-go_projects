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

// duplexPipe 创建一对双向的内存 ReadWriteCloser。
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
	// 使用默认目录初始化真实的技能管理器
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

	// 创建双工流，模拟子进程（如 AGY server）
	childToProxyR, childToProxyW := io.Pipe()
	proxyToChildR, proxyToChildW := io.Pipe()

	proxyChildStream := &duplexPipe{reader: childToProxyR, writer: proxyToChildW}
	mockChildStream := &duplexPipe{reader: proxyToChildR, writer: childToProxyW}

	// 创建写入模拟 Zed Stdout 的 Injector
	var zedStdout bytes.Buffer
	inj := NewInjector(&zedStdout)

	zedStdinR, zedStdinW := io.Pipe()
	defer zedStdinR.Close()
	defer zedStdinW.Close()

	runner := NewRunner("", nil, nil, skillMgr)
	runner.Stdin = zedStdinR

	// 在后台运行流处理
	go func() {
		_ = runner.RunStream(proxyChildStream, inj)
	}()

	// 1. 子进程返回 session/new 响应（模拟原生 agy 的 3 条命令）
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

	// 等待 Zed 收到增强后的响应
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
		SessionID         string                  `json:"sessionId"`
		AvailableCommands []protocol.CommandItem  `json:"availableCommands"`
		ConfigOptions     []protocol.ConfigOption `json:"configOptions"`
	}
	if err := json.Unmarshal(zedNewResp.Result, &zedResult); err != nil {
		t.Fatalf("failed to parse zed availableCommands: %v", err)
	}

	if zedResult.SessionID != "sess_test_123" {
		t.Fatalf("unexpected sessionId: %s", zedResult.SessionID)
	}

	// 应包含原生 3 条 + 3 条官方思维增强命令(grill-me, goal, compact) + 技能
	if len(zedResult.AvailableCommands) < 7 {
		t.Fatalf("expected >= 7 commands, got %d", len(zedResult.AvailableCommands))
	}

	foundPlan := false
	foundGrillMe := false
	foundGoal := false
	foundCompact := false
	foundGenericSkill := false
	for _, c := range zedResult.AvailableCommands {
		switch c.Name {
		case "plan":
			foundPlan = true
		case "grill-me":
			foundGrillMe = true
		case "goal":
			foundGoal = true
		case "compact":
			foundCompact = true
		case "skill":
			foundGenericSkill = true
		}
	}
	if !foundPlan {
		t.Fatal("native 'plan' command lost!")
	}
	if !foundGrillMe || !foundGoal || !foundCompact {
		t.Fatalf("native augmented commands lost! grill-me=%v, goal=%v, compact=%v", foundGrillMe, foundGoal, foundCompact)
	}
	if foundGenericSkill {
		t.Fatal("unexpected generic 'skill' command found in list!")
	}

	// 验证 Workflow 下拉框配置注入
	foundWorkflow := false
	for _, opt := range zedResult.ConfigOptions {
		if opt.ID == "workflow" {
			foundWorkflow = true
			if len(opt.Options) != 2 || opt.Options[1].Value != "plan" {
				t.Fatalf("unexpected workflow options: %+v", opt.Options)
			}
		}
	}
	if !foundWorkflow {
		t.Fatal("expected workflow config option injected into Zed session/new response")
	}

	// 2. 入向测试：用户通过带前缀的 /skill:name 调用技能
	userPromptPrefixed := []byte(`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"sess_test_123","prompt":"/skill:sh-pocock-workflow 帮我审查架构"}}` + "\n")
	processedPrefixed := runner.processInbound(userPromptPrefixed, inj)

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

	// 2b. 入向测试：用户通过通用 /skill name 调用技能
	userPromptReq := []byte(`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"sess_test_123","prompt":"/skill sh-pocock-workflow 帮我审查架构"}}` + "\n")
	processedInbound := runner.processInbound(userPromptReq, inj)

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

	// 3. 入向测试：用户调用原生 /plan 命令
	planReq := []byte(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"sess_test_123","prompt":"/plan 仔细规划"}}` + "\n")
	processedPlan := runner.processInbound(planReq, inj)

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
