package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testToken = "8db97e3da78c44abb0a72ce3ae423209.xK2juMP0d8xb236g"

// TestMain 隔离数据目录（AppData → 临时目录）并把上游指到进程内 httptest
// （service.Open 经 GLM_QUOTA_WATCH_API_BASE 注入），绝不触网、不碰真实配置。
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "gqw-mcp-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("AppData", tmp)

	var pct atomic.Int64
	pct.Store(2)
	reset := time.Now().Add(4*time.Hour + 43*time.Minute).UnixMilli()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"code":200,"msg":"操作成功","success":true,"data":{"level":"max","limits":[`+
			`{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":%d,"nextResetTime":%d}]}}`,
			pct.Load(), reset)
	}))
	defer srv.Close()
	os.Setenv("GLM_QUOTA_WATCH_API_BASE", srv.URL)

	code := m.Run()
	srv.Close()
	os.Exit(code)
}

// newTestSession 建立 in-memory MCP 连接。
func newTestSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	server := NewServer()
	client := mcp.NewClient(&mcp.Implementation{Name: "gqw-mcp-test"}, nil)
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

// call 调用工具并把 structuredContent 反序列化为 T。
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

// errText IsError 结果的首条文本内容（期望 "code: message" 形态）。
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

type configView struct {
	Token      string `json:"token"`
	HasToken   bool   `json:"has_token"`
	Interval   string `json:"interval"`
	Thresholds []int  `json:"thresholds"`
	Hysteresis int    `json:"hysteresis"`
	Silent     bool   `json:"silent"`
	Dir        string `json:"dir"`
}

type statusView struct {
	Level     string `json:"level"`
	SampledAt string `json:"sampled_at"`
	Windows   []struct {
		Key        string `json:"key"`
		Label      string `json:"label"`
		Percentage int64  `json:"percentage"`
		ResetIn    string `json:"reset_in"`
	} `json:"windows"`
}

func TestToolsListed(t *testing.T) {
	cs := newTestSession(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools 失败: %v", err)
	}
	if len(res.Tools) != 6 {
		t.Fatalf("期望 6 个工具，实际 %d", len(res.Tools))
	}
	for _, tool := range res.Tools {
		if tool.InputSchema == nil {
			t.Fatalf("工具 %s 缺 inputSchema", tool.Name)
		}
		for _, sc := range []struct {
			label string
			v     any
		}{
			{"inputSchema", tool.InputSchema},
			{"outputSchema", tool.OutputSchema},
		} {
			if sc.v == nil {
				continue
			}
			b, err := json.Marshal(sc.v)
			if err != nil {
				t.Fatalf("工具 %s %s 序列化失败: %v", tool.Name, sc.label, err)
			}
			if strings.Contains(string(b), `"type":[`) {
				t.Fatalf("工具 %s %s 含数组形式 type（type-union），应拆为单类型", tool.Name, sc.label)
			}
		}
	}
}

func TestStatusNoToken(t *testing.T) {
	cs := newTestSession(t)
	// 先清掉其他用例可能写入的 token，保证起点干净（不依赖执行顺序）
	if _, err := mustSvc(); err != nil {
		t.Fatalf("mustSvc: %v", err)
	}
	if _, err := svcInst.RemoveToken(); err != nil {
		t.Fatalf("RemoveToken: %v", err)
	}
	res, _ := call[statusView](t, cs, "gqw.status", map[string]any{})
	if txt := errText(t, res); !strings.HasPrefix(txt, "no_token:") {
		t.Fatalf("应报 no_token: %s", txt)
	}
}

func TestFullFlow(t *testing.T) {
	cs := newTestSession(t)

	// token.set → 返回脱敏视图
	setRes, cfg := call[configView](t, cs, "gqw.token.set", map[string]any{"token": testToken})
	if setRes.IsError {
		t.Fatalf("token.set 失败: %s", errText(t, setRes))
	}
	if !cfg.HasToken || strings.Contains(cfg.Token, testToken) {
		t.Fatalf("token.set 视图应脱敏: %+v", cfg)
	}

	// token.show → 同样返回脱敏视图（与 CLI token show 同形）
	tsRes, tsv := call[configView](t, cs, "gqw.token.show", map[string]any{})
	if tsRes.IsError || !tsv.HasToken || strings.Contains(tsv.Token, testToken) {
		t.Fatalf("token.show 应脱敏: %+v", tsv)
	}

	// status → 成功采样（httptest 上游）
	stRes, sv := call[statusView](t, cs, "gqw.status", map[string]any{})
	if stRes.IsError {
		t.Fatalf("status 失败: %s", errText(t, stRes))
	}
	if sv.Level != "max" || len(sv.Windows) != 1 || sv.Windows[0].Percentage != 2 {
		t.Fatalf("status 结果不符: %+v", sv)
	}
	if sv.Windows[0].ResetIn != "4h43m" {
		t.Fatalf("reset_in 不符: %+v", sv.Windows[0])
	}

	// config.show → token 脱敏
	gcRes, cfg := call[configView](t, cs, "gqw.config.show", map[string]any{})
	if gcRes.IsError || !cfg.HasToken || strings.Contains(cfg.Token, testToken) {
		t.Fatalf("config.show 应脱敏: %+v", cfg)
	}

	// config.set → 合法与非法
	scRes, cfg := call[configView](t, cs, "gqw.config.set", map[string]any{"key": "interval", "value": "90s"})
	if scRes.IsError || cfg.Interval != "1m30s" {
		t.Fatalf("config.set interval 不符: %+v err=%s", cfg, errText(t, scRes))
	}
	badRes, _ := call[configView](t, cs, "gqw.config.set", map[string]any{"key": "foo", "value": "1"})
	if txt := errText(t, badRes); !strings.HasPrefix(txt, "unknown_key:") {
		t.Fatalf("未知键应 unknown_key: %s", txt)
	}

	// token.remove → 幂等成功
	rmRes, rm := call[struct {
		Removed bool `json:"removed"`
	}](t, cs, "gqw.token.remove", map[string]any{})
	if rmRes.IsError || !rm.Removed {
		t.Fatalf("token.remove 应成功: %s", errText(t, rmRes))
	}
	_, cfg = call[configView](t, cs, "gqw.config.show", map[string]any{})
	if cfg.HasToken {
		t.Fatalf("remove 后应无 token: %+v", cfg)
	}
}

func TestSetTokenValidation(t *testing.T) {
	cs := newTestSession(t)
	res, _ := call[configView](t, cs, "gqw.token.set", map[string]any{"token": "short"})
	if txt := errText(t, res); !strings.HasPrefix(txt, "bad_args:") {
		t.Fatalf("短 token 应 bad_args: %s", txt)
	}
}
