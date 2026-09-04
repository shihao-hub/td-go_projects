package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"aiquick/internal/api"
	"aiquick/internal/protocol"
)

// ---- 构建与隔离 ----

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

// buildBackendD 构建真实 aiquickd.exe（每个测试进程只构建一次）。
func buildBackendD(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		out, err := exec.Command("go", "env", "GOMOD").Output()
		if err != nil {
			buildErr = fmt.Errorf("go env GOMOD: %w", err)
			return
		}
		root := filepath.Dir(strings.TrimSpace(string(out)))
		dir, err := os.MkdirTemp("", "aiquick-e2e-*")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "aiquickd.exe")
		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/aiquickd")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build: %w\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatalf("build aiquickd: %v", buildErr)
	}
	return binPath
}

// isolatedEnv 用临时目录替换 APPDATA，隔离后端的用户数据目录。
func isolatedEnv(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(strings.ToLower(kv), "appdata=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "APPDATA="+dir)
}

func startIsolated(t *testing.T) *Client {
	t.Helper()
	c, err := Start(buildBackendD(t), nil, WithEnv(isolatedEnv(t)))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(c.Shutdown)
	return c
}

// fakeLLM 同 backend 测试：normal 两段增量；hang 一段后挂住。
func fakeLLM(t *testing.T, mode string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		if mode == "normal" {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"答案\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"完毕\"}}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"第一段\"}}]}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func ctxTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func TestE2EHandshakeAndCRUD(t *testing.T) {
	c := startIsolated(t)

	if h := c.Hello(); h.Proto != protocol.Version || h.Name != "aiquickd" {
		t.Fatalf("hello wrong: %+v", h)
	}
	if c.State() != StateConnected {
		t.Fatal("should be connected")
	}

	ctx, cancel := ctxTimeout()
	defer cancel()
	if _, err := c.Call(ctx, "ping", nil, nil); err != nil {
		t.Fatalf("ping: %v", err)
	}

	var ps []api.Preset
	if _, err := c.Call(ctx, "presets.list", nil, &ps); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ps) != 3 {
		t.Fatalf("want 3 seeds, got %d", len(ps))
	}

	saved := api.Preset{ID: "e2e1", Name: "测试", System: "S", UserTemplate: "{{input}}!"}
	if _, err := c.Call(ctx, "presets.save", saved, nil); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := c.Call(ctx, "presets.list", nil, &ps); err != nil {
		t.Fatalf("list2: %v", err)
	}
	if len(ps) != 4 {
		t.Fatalf("want 4 presets, got %d", len(ps))
	}

	// 错误透传
	if _, err := c.Call(ctx, "no-such-method", nil, nil); err == nil {
		t.Fatal("unknown method should error")
	}
}

func TestE2EAskStreamAndCancel(t *testing.T) {
	srv := fakeLLM(t, "normal")
	c := startIsolated(t)
	ctx, cancel := ctxTimeout()
	defer cancel()

	if _, err := c.Call(ctx, "config.set", api.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"}, nil); err != nil {
		t.Fatalf("config.set: %v", err)
	}

	chunks := make(chan api.ChunkData, 10)
	c.Subscribe(api.EventChunk, func(ev protocol.Event) {
		var d api.ChunkData
		_ = json.Unmarshal(ev.Data, &d)
		chunks <- d
	})

	var askRid int64
	var res api.AskResult
	rid, err := c.CallStream(ctx, "ask.stream", api.AskParams{PresetID: "seed-trans", Input: "hi"}, &res, func(id int64) { askRid = id })
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if rid != askRid {
		t.Fatalf("onRID mismatch: %d vs %d", rid, askRid)
	}
	if res.Text != "答案完毕" {
		t.Fatalf("final text wrong: %q", res.Text)
	}
	// 收集全部 chunk（500ms 静默即认为收完），拼接应等于最终文本
	var parts []string
	for {
		var d api.ChunkData
		select {
		case d = <-chunks:
			parts = append(parts, d.Text)
			continue
		case <-time.After(500 * time.Millisecond):
		}
		break
	}
	if strings.Join(parts, "") != res.Text {
		t.Fatalf("chunks %q != final %q", parts, res.Text)
	}

	// ---- 取消场景 ----
	hang := fakeLLM(t, "hang")
	if _, err := c.Call(ctx, "config.set", api.Config{BaseURL: hang.URL, APIKey: "k", Model: "m"}, nil); err != nil {
		t.Fatalf("config.set2: %v", err)
	}
	gotFirst := make(chan struct{})
	c.Subscribe(api.EventChunk, func(ev protocol.Event) {
		select {
		case <-gotFirst:
		default:
			close(gotFirst)
		}
	})
	askRid2 := make(chan int64, 1)
	askDone := make(chan error, 1)
	go func() {
		_, err := c.CallStream(ctx, "ask.stream", api.AskParams{PresetID: "seed-trans", Input: "x"}, nil, func(id int64) { askRid2 <- id })
		askDone <- err
	}()
	select {
	case <-gotFirst:
	case <-time.After(5 * time.Second):
		t.Fatal("no first chunk")
	}
	var hangRID int64
	select {
	case hangRID = <-askRid2:
	default:
		t.Fatal("rid not reported")
	}
	if _, err := c.Call(ctx, "ask.cancel", api.CancelParams{RID: hangRID}, nil); err != nil {
		t.Fatalf("cancel call: %v", err)
	}
	select {
	case err := <-askDone:
		pe, ok := err.(*protocol.Error)
		if !ok || pe.Code != protocol.CodeCancelled {
			t.Fatalf("want ERR_CANCELLED, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ask not cancelled")
	}
}

func TestE2ELazyRestart(t *testing.T) {
	c := startIsolated(t)
	ctx, cancel := ctxTimeout()
	defer cancel()

	if _, err := c.Call(ctx, "presets.save", api.Preset{ID: "keep1", Name: "K", System: "S"}, nil); err != nil {
		t.Fatalf("save: %v", err)
	}

	pid := c.Pid()
	if pid == 0 {
		t.Fatal("no pid")
	}
	// 强杀后端
	kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	if out, err := kill.CombinedOutput(); err != nil {
		t.Fatalf("taskkill: %v\n%s", err, out)
	}

	// 下一次 Call 触发懒重启（读循环发现死亡前后有竞态，允许短暂失败重试）
	var err error
	deadline := time.Now().Add(15 * time.Second)
	for {
		pctx, pcancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err = c.Call(pctx, "ping", nil, nil)
		pcancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart failed: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if c.State() != StateConnected {
		t.Fatal("should reconnect")
	}
	if c.Pid() == pid {
		t.Fatal("should be a new process")
	}

	// 数据跨重启持久
	lctx, lcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer lcancel()
	var ps []api.Preset
	if _, err := c.Call(lctx, "presets.list", nil, &ps); err != nil {
		t.Fatalf("list after restart: %v", err)
	}
	if len(ps) != 4 {
		t.Fatalf("want 4 presets after restart, got %d", len(ps))
	}
}

func TestE2EShutdownThenClosed(t *testing.T) {
	bin := buildBackendD(t)
	c, err := Start(bin, nil, WithEnv(isolatedEnv(t)))
	if err != nil {
		t.Fatal(err)
	}
	pid := c.Pid()
	c.Shutdown()

	deadline := time.Now().Add(5 * time.Second)
	for c.State() != StateDisconnected {
		if time.Now().After(deadline) {
			t.Fatal("should disconnect after shutdown")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// 进程应已退出
	if out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid)).CombinedOutput(); err == nil {
		if !strings.Contains(string(out), "No tasks are running") {
			// tasklist 中文系统输出不同；容忍误判，仅记录
			t.Logf("tasklist says: %s", out)
		}
	}
	// 关闭后 Call 应报 ErrClosed
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.Call(ctx, "ping", nil, nil); err != ErrClosed {
		t.Fatalf("want ErrClosed, got %v", err)
	}
}
