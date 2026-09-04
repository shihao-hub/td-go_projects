package client

import (
	"testing"
	"time"

	"aiquick/internal/protocol"
)

func newTestClient() *Client {
	return &Client{
		pending: make(map[int64]chan protocol.Response),
		subs:    make(map[string][]EventCallback),
		deadCh:  make(chan struct{}),
	}
}

func TestDeliverResponseWakesCaller(t *testing.T) {
	c := newTestClient()
	ch := make(chan protocol.Response, 1)
	c.pending[42] = ch

	go c.deliverResponse(protocol.Response{ID: 42, OK: true, Result: []byte(`{"x":1}`)})
	select {
	case resp := <-ch:
		if !resp.OK || string(resp.Result) != `{"x":1}` {
			t.Fatalf("resp wrong: %+v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("response not delivered")
	}
	if _, still := c.pending[42]; still {
		t.Fatal("pending should be removed after delivery")
	}
}

func TestDeliverResponseUnknownIDIgnored(t *testing.T) {
	c := newTestClient()
	c.deliverResponse(protocol.Response{ID: 99, OK: true}) // 不应 panic
}

func TestDispatchEventToSubscribers(t *testing.T) {
	c := newTestClient()
	got := make(chan protocol.Event, 2)
	unsub := c.Subscribe("chunk", func(ev protocol.Event) { got <- ev })
	c.Subscribe("chunk", func(ev protocol.Event) { got <- ev }) // 第二个订阅者
	c.Subscribe("other", func(ev protocol.Event) { t.Error("should not fire") })

	c.dispatchEvent(protocol.Event{Event: "chunk", RID: 7, Data: []byte(`"d"`)})
	for i := 0; i < 2; i++ {
		select {
		case ev := <-got:
			if ev.RID != 7 || ev.Event != "chunk" {
				t.Fatalf("event wrong: %+v", ev)
			}
		case <-time.After(time.Second):
			t.Fatal("event not dispatched")
		}
	}

	unsub()
	c.dispatchEvent(protocol.Event{Event: "chunk"})
	if n := len(c.subs["chunk"]); n != 1 {
		t.Fatalf("unsubscribe failed, left %d", n)
	}
}

func TestHandleExitFailsPending(t *testing.T) {
	c := newTestClient()
	ch := make(chan protocol.Response, 1)
	c.pending[1] = ch

	go c.handleExit()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("channel should be closed, not deliver")
		}
	case <-time.After(time.Second):
		t.Fatal("pending not closed on exit")
	}
	if c.State() != StateDisconnected {
		t.Fatal("state should be disconnected")
	}
}

func TestStateChangeCallback(t *testing.T) {
	c := newTestClient()
	changed := make(chan State, 2)
	c.SetOnState(func(s State) { changed <- s })
	c.setState(StateConnected)
	select {
	case s := <-changed:
		if s != StateConnected {
			t.Fatalf("want connected, got %v", s)
		}
	case <-time.After(time.Second):
		t.Fatal("state callback not fired")
	}
	// 重复设置同状态不应再触发
	c.setState(StateConnected)
	select {
	case s := <-changed:
		t.Fatalf("duplicate callback: %v", s)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestMarshalParams(t *testing.T) {
	if raw, err := marshalParams(nil); err != nil || raw != nil {
		t.Fatalf("nil params should be nil: %v %v", raw, err)
	}
	type P struct {
		A string `json:"a"`
	}
	raw, err := marshalParams(P{A: "x"})
	if err != nil || string(raw) != `{"a":"x"}` {
		t.Fatalf("struct params wrong: %s %v", raw, err)
	}
	if raw, err := marshalParams([]byte(`{"b":1}`)); err != nil || string(raw) != `{"b":1}` {
		t.Fatalf("raw params wrong: %s %v", raw, err)
	}
}

func TestResolveBackendNotFound(t *testing.T) {
	if _, err := ResolveBackend(`Z:\definitely\not\exist\aiquickd.exe`); err == nil {
		t.Fatal("expected error")
	}
}
