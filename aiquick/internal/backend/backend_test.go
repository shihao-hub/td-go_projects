package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"aiquick/internal/api"
	"aiquick/internal/protocol"
	"aiquick/internal/server"
	"aiquick/internal/store"
)

// ---- 测试脚手架：io.Pipe 交互式会话 ----

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
	s := strings.TrimSpace(b.buf.String())
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

type session struct {
	out *safeBuffer
	in  *io.PipeWriter
}

// start 启动 Serve 会话；close(in) 后 Serve 读到 EOF 退出。
func start(t *testing.T, b *Backend) *session {
	t.Helper()
	pr, pw := io.Pipe()
	s := &session{out: &safeBuffer{}, in: pw}
	go func() { _ = server.Serve(t.Context(), pr, s.out, b.Router()) }()
	t.Cleanup(func() { _ = pw.Close() })
	return s
}

func (s *session) send(t *testing.T, line string) {
	t.Helper()
	if _, err := s.in.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("send: %v", err)
	}
}

// waitTotal 等输出累计达到 n 行并解码全部。
func waitTotal(t *testing.T, s *session, n int) []protocol.Envelope {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ls := s.out.lines(); len(ls) >= n {
			envs := make([]protocol.Envelope, 0, len(ls))
			for _, l := range ls {
				env, err := protocol.DecodeEnvelope([]byte(l))
				if err != nil {
					t.Fatalf("bad line %q: %v", l, err)
				}
				envs = append(envs, env)
			}
			return envs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting %d lines, got %q", n, s.out.lines())
	return nil
}

func byID(envs []protocol.Envelope, id int64) protocol.Envelope {
	for _, e := range envs {
		if e.ID == id && !e.IsEvent() {
			return e
		}
	}
	return protocol.Envelope{}
}

// fakeLLM 伪 OpenAI 兼容端点；normal 返回两段增量，hang 发一段后挂住。
func fakeLLM(t *testing.T, mode string) (*httptest.Server, *string) {
	t.Helper()
	var mu sync.Mutex
	body := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(raw)
		mu.Unlock()
		flusher := w.(http.Flusher)
		if mode == "normal" {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"，世界\"}}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
		} else {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"第一段\"}}]}\n\n")
			flusher.Flush()
			<-r.Context().Done()
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &body
}

func decodeInto(t *testing.T, raw []byte, v any) {
	t.Helper()
	if len(raw) == 0 {
		t.Fatal("empty result")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

// ---- 用例 ----

func newBackend(t *testing.T) *Backend {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(st)
}

// configure 发送 config.set 并等待成功。
func configure(t *testing.T, s *session, url string) {
	t.Helper()
	s.send(t, `{"id":1,"method":"config.set","params":{"baseURL":"`+url+`","apiKey":"sk-t","model":"m1"}}`)
	envs := waitTotal(t, s, 1)
	if e := byID(envs, 1); e.OK == nil || !*e.OK {
		t.Fatalf("config.set failed: %+v", e)
	}
}

func TestAskStreamWithSeedPreset(t *testing.T) {
	srv, body := fakeLLM(t, "normal")
	b := newBackend(t)
	s := start(t, b)
	configure(t, s, srv.URL)

	s.send(t, `{"id":2,"method":"ask.stream","params":{"presetId":"seed-trans","input":"hello"}}`)
	envs := waitTotal(t, s, 3) // config resp + chunk + ask resp

	if !envs[1].IsEvent() || envs[1].RID != 2 || envs[1].Event != api.EventChunk {
		t.Fatalf("chunk event wrong: %+v", envs[1])
	}
	var cd api.ChunkData
	decodeInto(t, envs[1].Data, &cd)
	if cd.Text != "你好" {
		t.Fatalf("chunk text wrong: %q", cd.Text)
	}
	e := byID(envs, 2)
	if e.OK == nil || !*e.OK {
		t.Fatalf("ask failed: %+v", e)
	}
	var res api.AskResult
	decodeInto(t, e.Result, &res)
	if res.Text != "你好，世界" {
		t.Fatalf("final text wrong: %q", res.Text)
	}
	// 请求组装：system 来自种子预设，user 为原文
	if req := *body; !strings.Contains(req, `"role":"system"`) || !strings.Contains(req, "地道的简体中文") {
		t.Fatalf("system message missing: %s", req)
	} else if !strings.Contains(req, `"content":"hello"`) {
		t.Fatalf("user message wrong: %s", req)
	}
}

func TestAskStreamTemplatePreset(t *testing.T) {
	srv, body := fakeLLM(t, "normal")
	b := newBackend(t)
	s := start(t, b)
	configure(t, s, srv.URL)

	s.send(t, `{"id":2,"method":"presets.save","params":{"id":"tpl1","name":"术语","system":"翻译术语","userTemplate":"把 {{input}} 翻译成英文术语"}}`)
	waitTotal(t, s, 2)
	s.send(t, `{"id":3,"method":"ask.stream","params":{"presetId":"tpl1","input":"内存"}}`)
	envs := waitTotal(t, s, 4) // config resp / save resp / chunk / ask resp
	if e := byID(envs, 3); e.OK == nil || !*e.OK {
		t.Fatalf("ask failed: %+v", e)
	}
	if req := *body; !strings.Contains(req, `"content":"把 内存 翻译成英文术语"`) {
		t.Fatalf("template render wrong: %s", req)
	}
}

func TestAskCancel(t *testing.T) {
	srv, _ := fakeLLM(t, "hang")
	b := newBackend(t)
	s := start(t, b)
	configure(t, s, srv.URL)

	s.send(t, `{"id":2,"method":"ask.stream","params":{"presetId":"seed-trans","input":"x"}}`)
	waitTotal(t, s, 2) // config resp + 第一段 chunk
	s.send(t, `{"id":3,"method":"ask.cancel","params":{"rid":2}}`)
	s.send(t, `{"id":4,"method":"ping"}`)
	// 全部输出：config resp / chunk / cancel resp / ask 取消应答 / ping resp = 5 行
	envs := waitTotal(t, s, 5)

	if e := byID(envs, 4); e.OK == nil || !*e.OK {
		t.Fatalf("ping failed: %+v", e)
	}
	if e := byID(envs, 3); e.OK == nil || !*e.OK || !strings.Contains(string(e.Result), `"cancelled":true`) {
		t.Fatalf("cancel failed: %+v %s", e, e.Result)
	}
	e := byID(envs, 2)
	if e.Error == nil || e.Error.Code != protocol.CodeCancelled {
		t.Fatalf("want ERR_CANCELLED: %+v", e.Error)
	}
}

func TestAskWithoutAPIKey(t *testing.T) {
	b := newBackend(t)
	s := start(t, b)
	s.send(t, `{"id":1,"method":"ask.stream","params":{"presetId":"seed-trans","input":"x"}}`)
	envs := waitTotal(t, s, 1)
	if e := byID(envs, 1); e.Error == nil || e.Error.Code != protocol.CodeBadRequest || !strings.Contains(e.Error.Message, "API Key") {
		t.Fatalf("want bad request about API key: %+v", e.Error)
	}
}

func TestConfigValidation(t *testing.T) {
	b := newBackend(t)
	s := start(t, b)
	for i, line := range []string{
		`{"id":1,"method":"config.set","params":{"baseURL":"","apiKey":"k","model":"m"}}`,
		`{"id":2,"method":"config.set","params":{"baseURL":"http://x","apiKey":"k","model":""}}`,
		`{"id":3,"method":"config.set","params":{"baseURL":"ftp://x","apiKey":"k","model":"m"}}`,
	} {
		s.send(t, line)
		envs := waitTotal(t, s, i+1)
		if e := byID(envs, int64(i+1)); e.Error == nil || e.Error.Code != protocol.CodeBadRequest {
			t.Fatalf("case %d want bad request: %+v", i+1, e.Error)
		}
	}
}

func TestPresetsListAndDelete(t *testing.T) {
	b := newBackend(t)
	s := start(t, b)

	s.send(t, `{"id":1,"method":"presets.list"}`)
	envs := waitTotal(t, s, 1)
	var ps []api.Preset
	decodeInto(t, byID(envs, 1).Result, &ps)
	if len(ps) != 3 {
		t.Fatalf("want 3 seeds: %d", len(ps))
	}

	s.send(t, `{"id":2,"method":"presets.delete","params":{"id":"seed-var"}}`)
	envs = waitTotal(t, s, 2)
	if e := byID(envs, 2); e.OK == nil || !*e.OK {
		t.Fatalf("delete failed: %+v", e)
	}

	s.send(t, `{"id":3,"method":"presets.delete","params":{"id":"seed-var"}}`)
	envs = waitTotal(t, s, 3)
	if e := byID(envs, 3); e.Error == nil || e.Error.Code != protocol.CodeNotFound {
		t.Fatalf("delete missing want not found: %+v", e.Error)
	}

	s.send(t, `{"id":4,"method":"presets.save","params":{"name":"","system":"x"}}`)
	waitTotal(t, s, 4)
	s.send(t, `{"id":5,"method":"presets.save","params":{"name":"n"}}`)
	envs = waitTotal(t, s, 5)
	for _, id := range []int64{4, 5} {
		if e := byID(envs, id); e.Error == nil || e.Error.Code != protocol.CodeBadRequest {
			t.Fatalf("save %d want bad request: %+v", id, e.Error)
		}
	}
}

func TestHello(t *testing.T) {
	b := newBackend(t)
	b.Version = "vtest"
	s := start(t, b)
	s.send(t, `{"id":1,"method":"hello"}`)
	envs := waitTotal(t, s, 1)
	var h api.HelloResult
	decodeInto(t, byID(envs, 1).Result, &h)
	if h.Proto != protocol.Version || h.Name != "aiquickd" || h.Version != "vtest" {
		t.Fatalf("hello wrong: %+v", h)
	}
}
