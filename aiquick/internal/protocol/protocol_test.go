package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeLineEndsWithNewline(t *testing.T) {
	line, err := EncodeLine(Request{ID: 1, Method: "ping"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(line)
	if !strings.HasSuffix(s, "\n") {
		t.Fatalf("line should end with \\n, got %q", s)
	}
	if strings.Contains(s[:len(s)-1], "\n") {
		t.Fatalf("line should contain no inner newline: %q", s)
	}
}

func TestRequestOmitsEmptyParams(t *testing.T) {
	line, err := EncodeLine(Request{ID: 2, Method: "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(line), "params") {
		t.Fatalf("empty params should be omitted: %s", line)
	}
	line, err = EncodeLine(Request{ID: 2, Method: "m", Params: json.RawMessage(`{"a":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(line), `"params":{"a":1}`) {
		t.Fatalf("params should be embedded: %s", line)
	}
}

func TestResponseShape(t *testing.T) {
	line, err := EncodeLine(Response{ID: 3, OK: true, Result: json.RawMessage(`{"pong":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":3,"ok":true,"result":{"pong":true}}` + "\n"
	if string(line) != want {
		t.Fatalf("got %s want %s", line, want)
	}

	line, _ = EncodeLine(Response{ID: 4, OK: false, Error: &Error{Code: CodeNotFound, Message: "x"}})
	if !strings.Contains(string(line), `"error":{"code":"ERR_NOT_FOUND","message":"x"}`) {
		t.Fatalf("error shape wrong: %s", line)
	}
}

func TestEnvelopeRoundTrip(t *testing.T) {
	// Response 形态
	env, err := DecodeEnvelope([]byte(`{"id":7,"ok":true,"result":{"x":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if env.IsEvent() {
		t.Fatal("should not be event")
	}
	if env.ID != 7 || env.OK == nil || !*env.OK || string(env.Result) != `{"x":1}` {
		t.Fatalf("bad envelope: %+v", env)
	}

	// Event 形态（带 rid）
	env, err = DecodeEnvelope([]byte(`{"event":"chunk","rid":9,"data":{"text":"hi"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !env.IsEvent() || env.Event != "chunk" || env.RID != 9 || string(env.Data) != `{"text":"hi"}` {
		t.Fatalf("bad event envelope: %+v", env)
	}

	// Event 形态（无 rid，rid 省略）
	env, err = DecodeEnvelope([]byte(`{"event":"log","data":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !env.IsEvent() || env.RID != 0 {
		t.Fatalf("bad event envelope: %+v", env)
	}
}

func TestDecodeEnvelopeRejectsGarbage(t *testing.T) {
	if _, err := DecodeEnvelope([]byte(`not json`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestErrorMessage(t *testing.T) {
	e := &Error{Code: CodeInternal, Message: "boom"}
	if e.Error() != "ERR_INTERNAL: boom" {
		t.Fatalf("got %q", e.Error())
	}
}
