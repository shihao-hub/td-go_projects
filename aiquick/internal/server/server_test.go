package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"aiquick/internal/protocol"
)

// safeBuffer 并发安全的输出缓冲。
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := strings.Split(strings.TrimSpace(b.buf.String()), "\n")
	if len(out) == 1 && out[0] == "" {
		return nil
	}
	return out
}

// waitEnvelopes 轮询等待输出达到 n 行并解码。
func waitEnvelopes(t *testing.T, b *safeBuffer, n int) []protocol.Envelope {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if lines := b.lines(); len(lines) >= n {
			envs := make([]protocol.Envelope, 0, len(lines))
			for _, l := range lines {
				env, err := protocol.DecodeEnvelope([]byte(l))
				if err != nil {
					t.Fatalf("bad output line %q: %v", l, err)
				}
				envs = append(envs, env)
			}
			return envs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting %d lines, got %d: %q", n, len(b.lines()), b.lines())
	return nil
}

// runServe 在后台启动 Serve，input 为多行请求文本。
func runServe(t *testing.T, input string, r *Router) (*safeBuffer, chan error) {
	t.Helper()
	out := &safeBuffer{}
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), strings.NewReader(input), out, r) }()
	return out, done
}

func TestUnknownMethod(t *testing.T) {
	r := NewRouter()
	out, done := runServe(t, `{"id":1,"method":"nope"}`, r)
	envs := waitEnvelopes(t, out, 1)
	if envs[0].ID != 1 || envs[0].OK == nil || *envs[0].OK {
		t.Fatalf("bad response: %+v", envs[0])
	}
	if envs[0].Error == nil || envs[0].Error.Code != protocol.CodeNotFound {
		t.Fatalf("want ERR_NOT_FOUND: %+v", envs[0].Error)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve not ended on EOF")
	}
}

func TestMalformedLine(t *testing.T) {
	r := NewRouter()
	out, _ := runServe(t, "not-json\n{\"id\":1,\"method\":\"m\"}", r)
	envs := waitEnvelopes(t, out, 2)
	if envs[0].ID != 0 || envs[0].Error == nil || envs[0].Error.Code != protocol.CodeBadRequest {
		t.Fatalf("want ERR_BAD_REQUEST id=0: %+v", envs[0])
	}
}

func TestEchoResultAndEvents(t *testing.T) {
	r := NewRouter()
	r.Handle("echo", func(ctx context.Context, rid int64, params json.RawMessage, emit Emit) (any, error) {
		emit(api_EventChunk, "one")
		emit(api_EventChunk, "two")
		return map[string]json.RawMessage{"echo": params}, nil
	})
	out, done := runServe(t, `{"id":7,"method":"echo","params":{"msg":"hi"}}`, r)
	envs := waitEnvelopes(t, out, 3)

	if !envs[0].IsEvent() || envs[0].RID != 7 || string(envs[0].Data) != `"one"` {
		t.Fatalf("event1 wrong: %+v", envs[0])
	}
	if !envs[1].IsEvent() || envs[1].RID != 7 {
		t.Fatalf("event2 wrong: %+v", envs[1])
	}
	last := envs[2]
	if last.IsEvent() || last.ID != 7 || last.OK == nil || !*last.OK {
		t.Fatalf("response wrong: %+v", last)
	}
	if string(last.Result) != `{"echo":{"msg":"hi"}}` {
		t.Fatalf("result wrong: %s", last.Result)
	}
	<-done
}

const api_EventChunk = "chunk" // 测试用事件名，避免依赖业务包

func TestConcurrentDispatch(t *testing.T) {
	release := make(chan struct{})
	r := NewRouter()
	r.Handle("block", func(ctx context.Context, rid int64, params json.RawMessage, emit Emit) (any, error) {
		<-release
		return "blocked-done", nil
	})
	r.Handle(protocol.MethodPing, func(ctx context.Context, rid int64, params json.RawMessage, emit Emit) (any, error) {
		return nil, nil
	})
	out, done := runServe(t, "{\"id\":1,\"method\":\"block\"}\n{\"id\":2,\"method\":\"ping\"}", r)

	// ping 的应答必须先于 block 到达（block 还挂着）
	envs := waitEnvelopes(t, out, 1)
	if envs[0].ID != 2 || envs[0].OK == nil || !*envs[0].OK {
		t.Fatalf("ping should respond first: %+v", envs[0])
	}
	close(release)
	envs = waitEnvelopes(t, out, 2)
	if envs[1].ID != 1 || string(envs[1].Result) != `"blocked-done"` {
		t.Fatalf("block result wrong: %+v", envs[1])
	}
	<-done
}

func TestShutdownEndsServe(t *testing.T) {
	r := NewRouter()
	r.Handle(protocol.MethodPing, func(ctx context.Context, rid int64, params json.RawMessage, emit Emit) (any, error) {
		return nil, nil
	})
	out, done := runServe(t, "{\"id\":1,\"method\":\"ping\"}\n{\"id\":2,\"method\":\"shutdown\"}", r)
	envs := waitEnvelopes(t, out, 2)
	e := envs[0]
	if e.ID != 2 {
		e = envs[1] // ping 应答与 shutdown 应答顺序不定
	}
	if e.ID != 2 || e.OK == nil || !*e.OK {
		t.Fatalf("shutdown response wrong: %+v", envs)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve not ended after shutdown")
	}
}

func TestPanicRecovery(t *testing.T) {
	r := NewRouter()
	r.Handle("boom", func(ctx context.Context, rid int64, params json.RawMessage, emit Emit) (any, error) {
		panic("kaboom")
	})
	out, _ := runServe(t, `{"id":5,"method":"boom"}`, r)
	envs := waitEnvelopes(t, out, 1)
	if envs[0].ID != 5 || envs[0].Error == nil || envs[0].Error.Code != protocol.CodeInternal {
		t.Fatalf("panic should map to ERR_INTERNAL: %+v", envs[0])
	}
	if !strings.Contains(envs[0].Error.Message, "kaboom") {
		t.Fatalf("panic message lost: %+v", envs[0].Error)
	}
}

func TestCodedError(t *testing.T) {
	r := NewRouter()
	r.Handle("bad", func(ctx context.Context, rid int64, params json.RawMessage, emit Emit) (any, error) {
		return nil, Coded(protocol.CodeBadRequest, fmt.Errorf("名字不合法"))
	})
	out, _ := runServe(t, `{"id":9,"method":"bad"}`, r)
	envs := waitEnvelopes(t, out, 1)
	if envs[0].Error == nil || envs[0].Error.Code != protocol.CodeBadRequest || envs[0].Error.Message != "名字不合法" {
		t.Fatalf("coded error wrong: %+v", envs[0].Error)
	}
}

func TestNilResultOmitted(t *testing.T) {
	r := NewRouter()
	r.Handle(protocol.MethodPing, func(ctx context.Context, rid int64, params json.RawMessage, emit Emit) (any, error) {
		return nil, nil
	})
	out := &safeBuffer{}
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), strings.NewReader(`{"id":1,"method":"ping"}`), out, r) }()
	waitEnvelopes(t, out, 1)
	lines := out.lines()
	if strings.Contains(lines[0], "result") {
		t.Fatalf("nil result should be omitted: %s", lines[0])
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["ok"] != true {
		t.Fatalf("ok should be true: %s", lines[0])
	}
	<-done
}
