package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMain 隔离数据目录（AppData → 临时目录），mustSvc 单例只开这一次库，
// 全部用例共享隔离库，绝不触碰真实注册数据。
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "clictl-mcp-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("AppData", tmp)
	os.Exit(m.Run())
}

// newTestSession 建立 in-memory MCP 连接
func newTestSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	server := NewServer()
	client := mcp.NewClient(&mcp.Implementation{Name: "clictl-mcp-test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("连接 server 失败: %v", err)
	}
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("连接 client 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})
	return clientSession
}

// call 调用工具并把 structuredContent 反序列化为 T
func call[T any](t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, T) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s 协议级错误: %v", name, err)
	}
	var out T
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("%s structuredContent 反序列化失败: %v", name, err)
		}
	}
	return res, out
}

// errText IsError 结果的首条文本内容
func errText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if !res.IsError {
		t.Fatalf("期望 IsError=true，实际 false")
	}
	if len(res.Content) == 0 {
		t.Fatalf("期望错误内容非空")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("期望 TextContent，实际 %T", res.Content[0])
	}
	return tc.Text
}

// fakeExe 在临时目录创建一个假 .exe 文件（service 只校验存在性）
func fakeExe(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("MZ fake"), 0o644); err != nil {
		t.Fatalf("创建假 exe 失败: %v", err)
	}
	return p
}

func TestToolsListed(t *testing.T) {
	cs := newTestSession(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools 失败: %v", err)
	}
	if len(res.Tools) != 10 {
		t.Fatalf("期望 10 个工具，实际 %d", len(res.Tools))
	}
	for _, tool := range res.Tools {
		if tool.InputSchema == nil {
			t.Fatalf("工具 %s 缺 inputSchema", tool.Name)
		}
	}
}

func TestVersionTool(t *testing.T) {
	cs := newTestSession(t)
	res, out := call[versionOut](t, cs, "clictl.version", map[string]any{})
	if res.IsError {
		t.Fatalf("version 不应失败")
	}
	if out.Version == "" {
		t.Fatalf("version 为空")
	}
}

func TestCrudFlow(t *testing.T) {
	cs := newTestSession(t)
	exe := fakeExe(t, "mcpdemo.exe")

	// add（带 meta）
	addRes, tool := call[toolView](t, cs, "clictl.add", map[string]any{
		"path": exe,
		"meta": map[string]any{"source": "mcp-test", "tags": []any{"dev"}},
	})
	if addRes.IsError {
		t.Fatalf("add 失败: %s", errText(t, addRes))
	}
	if tool.Name != "mcpdemo" || tool.Meta["source"] != "mcp-test" {
		t.Fatalf("add 返回不符: %+v", tool)
	}

	// 重复注册 → conflict
	dupRes, _ := call[toolView](t, cs, "clictl.add", map[string]any{"path": exe})
	if txt := errText(t, dupRes); len(txt) == 0 {
		t.Fatalf("重复注册应报错")
	}

	// list 含新工具
	_, lo := call[listOut](t, cs, "clictl.list", map[string]any{})
	if len(lo.Tools) == 0 {
		t.Fatalf("list 应含 mcpdemo")
	}
	found := false
	for _, tv := range lo.Tools {
		if tv.Name == "mcpdemo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("list 未找到 mcpdemo")
	}

	// set 清空 meta
	setRes, updated := call[toolView](t, cs, "clictl.set", map[string]any{
		"name": "mcpdemo",
		"meta": map[string]any{},
	})
	if setRes.IsError {
		t.Fatalf("set 失败: %s", errText(t, setRes))
	}
	if updated.Meta != nil {
		t.Fatalf("set {} 后 meta 应清空，实际 %+v", updated.Meta)
	}

	// set 非法 key → isError
	badRes, _ := call[toolView](t, cs, "clictl.set", map[string]any{
		"name": "mcpdemo",
		"meta": map[string]any{"unknown_key": "x"},
	})
	if txt := errText(t, badRes); txt == "" {
		t.Fatalf("非法 meta key 应报错")
	}

	// info
	infoRes, info := call[infoOut](t, cs, "clictl.info", map[string]any{"name": "mcpdemo"})
	if infoRes.IsError {
		t.Fatalf("info 失败: %s", errText(t, infoRes))
	}
	if info.Tool.Name != "mcpdemo" {
		t.Fatalf("info 返回不符: %+v", info)
	}

	// list 互斥校验
	muxRes, _ := call[listOut](t, cs, "clictl.list", map[string]any{"status": "active", "running": true})
	if errText(t, muxRes) == "" {
		t.Fatalf("status 与 running 互斥应报错")
	}

	// rm
	rmRes, rmOut := call[struct {
		Removed int64  `json:"removed"`
		Name    string `json:"name"`
	}](t, cs, "clictl.rm", map[string]any{"name": "mcpdemo"})
	if rmRes.IsError {
		t.Fatalf("rm 失败: %s", errText(t, rmRes))
	}
	if rmOut.Removed != 1 {
		t.Fatalf("rm 应删除 1 条，实际 %d", rmOut.Removed)
	}

	// rm 未注册 → isError not_found
	nfRes, _ := call[struct{}](t, cs, "clictl.rm", map[string]any{"name": "mcpdemo"})
	if txt := errText(t, nfRes); txt == "" {
		t.Fatalf("rm 未注册应报错")
	}
}

func TestRunToolExitCodes(t *testing.T) {
	cs := newTestSession(t)
	// 注册真 cmd.exe 用于执行语义验证
	cmdPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if _, err := os.Stat(cmdPath); err != nil {
		t.Skipf("无 cmd.exe: %v", err)
	}
	addRes, _ := call[toolView](t, cs, "clictl.add", map[string]any{"path": cmdPath, "name": "mcp-cmd"})
	if addRes.IsError {
		t.Fatalf("add cmd 失败: %s", errText(t, addRes))
	}

	// 退出码 0
	okRes, okOut := call[runOut](t, cs, "clictl.run", map[string]any{
		"name": "mcp-cmd",
		"args": []any{"/c", "exit", "0"},
	})
	if okRes.IsError || okOut.ExitCode != 0 {
		t.Fatalf("exit 0 应成功: isError=%v out=%+v", okRes.IsError, okOut)
	}
	if okOut.Stdout != "" || okOut.Stderr != "" || okOut.DurationMs < 0 {
		t.Fatalf("exit 0 输出形状异常: %+v", okOut)
	}

	// 退出码 3：IsError + 完整结构化结果
	failRes, failOut := call[runOut](t, cs, "clictl.run", map[string]any{
		"name": "mcp-cmd",
		"args": []any{"/c", "exit", "3"},
	})
	if !failRes.IsError || failOut.ExitCode != 3 {
		t.Fatalf("exit 3 应 IsError 且退出码透传: isError=%v out=%+v", failRes.IsError, failOut)
	}

	// stdout 捕获
	echoRes, echoOut := call[runOut](t, cs, "clictl.run", map[string]any{
		"name": "mcp-cmd",
		"args": []any{"/c", "echo", "hello-mcp"},
	})
	if echoRes.IsError || echoOut.Stdout == "" {
		t.Fatalf("echo 输出应被捕获: %+v", echoOut)
	}

	// 超时：ping 挂 10s，timeout 500ms → timed_out + IsError
	toRes, toOut := call[runOut](t, cs, "clictl.run", map[string]any{
		"name":       "mcp-cmd",
		"args":       []any{"/c", "ping", "-n", "10", "127.0.0.1"},
		"timeout_ms": 500,
	})
	if !toRes.IsError || !toOut.TimedOut || toOut.Cancelled {
		t.Fatalf("应超时失败: isError=%v out=%+v", toRes.IsError, toOut)
	}

	// 未注册
	nfRes, _ := call[runOut](t, cs, "clictl.run", map[string]any{"name": "no-such-tool"})
	if txt := errText(t, nfRes); txt == "" {
		t.Fatalf("run 未注册应报错")
	}

	// 清理
	_, _ = call[struct{}](t, cs, "clictl.rm", map[string]any{"name": "mcp-cmd"})
}

func TestCpTool(t *testing.T) {
	cs := newTestSession(t)
	exe := fakeExe(t, "cpdemo.exe")
	destDir := t.TempDir()

	addRes, _ := call[toolView](t, cs, "clictl.add", map[string]any{"path": exe})
	if addRes.IsError {
		t.Fatalf("add 失败: %s", errText(t, addRes))
	}

	cpRes, cpOut := call[struct {
		Name string `json:"name"`
		Dest string `json:"dest"`
	}](t, cs, "clictl.cp", map[string]any{"name": "cpdemo", "dest_dir": destDir})
	if cpRes.IsError {
		t.Fatalf("cp 失败: %s", errText(t, cpRes))
	}
	if cpOut.Name != "cpdemo" {
		t.Fatalf("cp 返回不符: %+v", cpOut)
	}

	// 目标已存在且未 force → isError
	dupRes, _ := call[struct{}](t, cs, "clictl.cp", map[string]any{"name": "cpdemo", "dest_dir": destDir})
	if txt := errText(t, dupRes); txt == "" {
		t.Fatalf("cp 目标已存在应报错")
	}

	// force 覆盖成功
	fRes, _ := call[struct{}](t, cs, "clictl.cp", map[string]any{"name": "cpdemo", "dest_dir": destDir, "force": true})
	if fRes.IsError {
		t.Fatalf("cp --force 应成功: %s", errText(t, fRes))
	}

	_, _ = call[struct{}](t, cs, "clictl.rm", map[string]any{"name": "cpdemo"})
}

// TestRunToolCancellation 验证取消链路：client 侧 ctx 取消 → SDK 发
// cancelled 通知 → server handler ctx 取消 → 杀树收尾（启动记录闭环）。
func TestRunToolCancellation(t *testing.T) {
	cs := newTestSession(t)
	cmdPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if _, err := os.Stat(cmdPath); err != nil {
		t.Skipf("无 cmd.exe: %v", err)
	}
	addRes, _ := call[toolView](t, cs, "clictl.add", map[string]any{"path": cmdPath, "name": "mcp-cancel"})
	if addRes.IsError {
		t.Fatalf("add 失败: %s", errText(t, addRes))
	}
	t.Cleanup(func() {
		_, _ = call[struct{}](t, cs, "clictl.rm", map[string]any{"name": "mcp-cancel"})
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()
	_, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "clictl.run",
		Arguments: map[string]any{
			"name": "mcp-cancel",
			"args": []any{"/c", "ping", "-n", "10", "127.0.0.1"},
		},
	})
	if err == nil {
		t.Fatalf("client ctx 取消后 CallTool 应返回错误")
	}

	// server 侧应已完成收尾：最近启动记录 duration_ms 已闭环（非 null）
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, info := call[infoOut](t, cs, "clictl.info", map[string]any{"name": "mcp-cancel"})
		if len(info.RecentLaunches) > 0 {
			l := info.RecentLaunches[0]
			if l.DurationMs != nil {
				return // 闭环完成
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("取消后启动记录未闭环: %+v", info.RecentLaunches)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestLimitedBuffer(t *testing.T) {
	var lb limitedBuffer
	small := []byte("hello")
	if n, err := lb.Write(small); err != nil || n != len(small) {
		t.Fatalf("小块写入失败: n=%d err=%v", n, err)
	}
	if lb.truncated {
		t.Fatalf("未超限不应截断")
	}

	// 超限写入：声明全部消费、内容截到上限、标记截断
	var lb2 limitedBuffer
	big := bytes.Repeat([]byte("a"), maxStreamBytes+100)
	n, err := lb2.Write(big)
	if err != nil || n != len(big) {
		t.Fatalf("超限写入应声明全部消费: n=%d err=%v", n, err)
	}
	if !lb2.truncated {
		t.Fatalf("超限应标记 truncated")
	}
	if lb2.buf.Len() != maxStreamBytes {
		t.Fatalf("保留量应恰为上限，实际 %d", lb2.buf.Len())
	}
}
