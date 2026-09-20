package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shihao-hub/liteconf/internal/server"
)

// newTestHTTPServer 起一个挂真 server.NewMux 的 httptest server，
// store 落 t.TempDir()（不碰用户真实数据目录），返回其 URL。
func newTestHTTPServer(t *testing.T) string {
	t.Helper()
	st, err := server.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	srv := httptest.NewServer(server.NewMux(st, server.NewBroadcaster()))
	t.Cleanup(srv.Close)
	return srv.URL
}

// connectInMemory 经 in-memory transport 连接 MCP server：
// server 先连、client 后连（client 连接时完成 initialize）。
// 真实进程 stdio 行为另由构建冒烟覆盖，两者不互替。
func connectInMemory(t *testing.T, serverURL string) *mcp.ClientSession {
	t.Helper()
	srv, err := newServer(Config{ServerURL: serverURL, Version: "test"})
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// callToolAndDecode 调用工具并把 structuredContent 解码为 toolOutput[D]
func callToolAndDecode[D any](t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, toolOutput[D]) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: protocol error: %v", name, err)
	}
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("call %s: marshal structured content: %v", name, err)
		}
		var out toolOutput[D]
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("call %s: decode structured content %s: %v", name, b, err)
		}
		return res, out
	}
	return res, toolOutput[D]{}
}

// wantToolOK 断言成功分支：isError=false 且 ok=true
func wantToolOK[D any](t *testing.T, res *mcp.CallToolResult, out toolOutput[D]) toolOutput[D] {
	t.Helper()
	if res.IsError {
		t.Fatalf("want success, got isError=true (out: %#v)", out)
	}
	if !out.OK {
		t.Fatalf("want ok=true, got %#v", out)
	}
	return out
}

// wantToolErr 断言失败分支：isError=true、ok=false、code 匹配
func wantToolErr[D any](t *testing.T, res *mcp.CallToolResult, out toolOutput[D], code string) {
	t.Helper()
	if !res.IsError {
		t.Fatalf("want isError=true for code %q, got success (out: %#v)", code, out)
	}
	if out.OK || out.Error == nil {
		t.Fatalf("want ok=false with error, got %#v", out)
	}
	if out.Error.Code != code {
		t.Fatalf("want error code %q, got %q (%s)", code, out.Error.Code, out.Error.Message)
	}
}

// schemaRootType 取 Schema 的根 type 字段（Tool.InputSchema 为 any，
// 实际由 jsonschema-go 生成）
func schemaRootType(t *testing.T, s any) string {
	t.Helper()
	if s == nil {
		t.Fatal("schema is nil")
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	typ, _ := m["type"].(string)
	return typ
}

func TestToolsList(t *testing.T) {
	cs := connectInMemory(t, newTestHTTPServer(t))

	// 完整遍历 tools/list 分页（标准 9.1：一次调用不能证明完整发现）
	names := map[string]*mcp.Tool{}
	params := &mcp.ListToolsParams{}
	for {
		res, err := cs.ListTools(context.Background(), params)
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		for _, tl := range res.Tools {
			names[tl.Name] = tl
		}
		if res.NextCursor == "" {
			break
		}
		params.Cursor = res.NextCursor
	}

	if len(names) != 3 {
		t.Fatalf("want 3 tools, got %d: %v", len(names), names)
	}
	for _, want := range []string{toolDiscovery, toolGetConfig, toolPutConfig} {
		if _, ok := names[want]; !ok {
			t.Fatalf("tool %q missing", want)
		}
	}

	get := names[toolGetConfig]
	if typ := schemaRootType(t, get.InputSchema); typ != "object" {
		t.Fatalf("get inputSchema root type = %q, want object", typ)
	}
	if typ := schemaRootType(t, get.OutputSchema); typ != "object" {
		t.Fatalf("get outputSchema root type = %q, want object", typ)
	}

	// 行为标注
	if ann := names[toolDiscovery].Annotations; ann == nil || !ann.ReadOnlyHint {
		t.Fatalf("discovery annotations: %#v", names[toolDiscovery].Annotations)
	}
	if ann := names[toolGetConfig].Annotations; ann == nil || !ann.ReadOnlyHint {
		t.Fatalf("get annotations: %#v", names[toolGetConfig].Annotations)
	}
	ann := names[toolPutConfig].Annotations
	if ann == nil || ann.ReadOnlyHint || ann.IdempotentHint || ann.DestructiveHint == nil || !*ann.DestructiveHint {
		t.Fatalf("put annotations: %#v", ann)
	}
}

func TestCallDiscoveryEmpty(t *testing.T) {
	cs := connectInMemory(t, newTestHTTPServer(t))
	res, out := callToolAndDecode[discoveryData](t, cs, toolDiscovery, nil)
	got := wantToolOK(t, res, out)
	if len(got.Data.Apps) != 0 {
		t.Fatalf("want empty apps, got %#v", got.Data.Apps)
	}
}

func TestCallFullFlow(t *testing.T) {
	cs := connectInMemory(t, newTestHTTPServer(t))
	ctx := context.Background()

	// put → version 1
	res, out := callToolAndDecode[configPutData](t, cs, toolPutConfig, map[string]any{
		"app": "app1", "env": "dev",
		"content": map[string]any{"db": map[string]any{"host": "127.0.0.1"}},
	})
	got := wantToolOK(t, res, out)
	if got.Data.Version != 1 || got.Data.App != "app1" || got.Data.Env != "dev" {
		t.Fatalf("put #1 result: %#v", got.Data)
	}

	// put again → version 2
	res, out = callToolAndDecode[configPutData](t, cs, toolPutConfig, map[string]any{
		"app": "app1", "env": "dev",
		"content": map[string]any{"db": map[string]any{"host": "10.0.0.1"}},
	})
	got = wantToolOK(t, res, out)
	if got.Data.Version != 2 {
		t.Fatalf("put #2 version: %#v", got.Data)
	}

	// get 整份
	res, out2 := callToolAndDecode[configGetData](t, cs, toolGetConfig, map[string]any{"app": "app1", "env": "dev"})
	got2 := wantToolOK(t, res, out2)
	if got2.Data.Version != 2 {
		t.Fatalf("get version: %#v", got2.Data)
	}
	content, ok := got2.Data.Content.(map[string]any)
	if !ok {
		t.Fatalf("get content not object: %#v", got2.Data.Content)
	}
	db, _ := content["db"].(map[string]any)
	if db == nil || db["host"] != "10.0.0.1" {
		t.Fatalf("get content mismatch: %#v", content)
	}

	// get 点路径命中
	res, out2 = callToolAndDecode[configGetData](t, cs, toolGetConfig, map[string]any{"app": "app1", "env": "dev", "path": "db.host"})
	got2 = wantToolOK(t, res, out2)
	if got2.Data.Content != "10.0.0.1" {
		t.Fatalf("get path content: %#v", got2.Data.Content)
	}

	// discovery 汇总
	res, out3 := callToolAndDecode[discoveryData](t, cs, toolDiscovery, nil)
	got3 := wantToolOK(t, res, out3)
	if len(got3.Data.Apps) != 1 || got3.Data.Apps[0].App != "app1" || got3.Data.Apps[0].Envs[0].Version != 2 {
		t.Fatalf("discovery: %#v", got3.Data.Apps)
	}
	_ = ctx
}

func TestCallErrorBranches(t *testing.T) {
	cs := connectInMemory(t, newTestHTTPServer(t))

	// 前置：写一份配置，path 未命中才不会先命中 not_found
	res, out1 := callToolAndDecode[configPutData](t, cs, toolPutConfig, map[string]any{
		"app": "app1", "env": "dev", "content": map[string]any{"db": map[string]any{"host": "127.0.0.1"}},
	})
	wantToolOK(t, res, out1)

	// path 未命中
	res, out := callToolAndDecode[configGetData](t, cs, toolGetConfig, map[string]any{"app": "app1", "env": "dev", "path": "nope"})
	wantToolErr(t, res, out, CodePathNotFound)

	// 配置不存在
	res, out = callToolAndDecode[configGetData](t, cs, toolGetConfig, map[string]any{"app": "nope", "env": "dev"})
	wantToolErr(t, res, out, "not_found")

	// 名称非法（由 server 校验，MCP 透传）
	res, out = callToolAndDecode[configGetData](t, cs, toolGetConfig, map[string]any{"app": "app1", "env": "bad name!"})
	wantToolErr(t, res, out, "invalid_name")

	// put 非法名称
	res, outPut := callToolAndDecode[configPutData](t, cs, toolPutConfig, map[string]any{
		"app": "bad/name", "env": "dev", "content": map[string]any{"k": "v"},
	})
	wantToolErr(t, res, outPut, "invalid_name")
}

func TestCallServerUnreachable(t *testing.T) {
	cs := connectInMemory(t, "http://127.0.0.1:1")
	res, out := callToolAndDecode[discoveryData](t, cs, toolDiscovery, nil)
	wantToolErr(t, res, out, CodeUnreachable)
}

func TestCallMissingRequired(t *testing.T) {
	// SDK 在进 handler 前按 inputSchema 拒绝缺参：
	// IsError=true 的工具结果，无 structuredContent（v1.8.0 server.go L402-407 已核对）
	cs := connectInMemory(t, newTestHTTPServer(t))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: toolGetConfig,
	})
	if err != nil {
		t.Fatalf("missing required should be tool error, got protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("want isError=true for missing required, got %#v", res)
	}
	if res.StructuredContent != nil {
		t.Fatalf("want no structuredContent for schema rejection, got %#v", res.StructuredContent)
	}
}
