package imap

import (
	"testing"
	"time"
)

func noopHandler(uint32, []byte) error { return nil }

func TestNewAppliesDefaults(t *testing.T) {
	listener, err := New(Options{
		Username:  "user@qq.com",
		Password:  "auth-code",
		OnMessage: noopHandler,
	})
	if err != nil {
		t.Fatalf("New() 错误: %v", err)
	}
	if listener.opts.Host != "imap.qq.com" {
		t.Errorf("Host = %q", listener.opts.Host)
	}
	if listener.opts.Port != 993 {
		t.Errorf("Port = %d", listener.opts.Port)
	}
	if listener.opts.Mailbox != "INBOX" {
		t.Errorf("Mailbox = %q", listener.opts.Mailbox)
	}
	if listener.opts.IdleTimeout != 25*time.Minute {
		t.Errorf("IdleTimeout = %v", listener.opts.IdleTimeout)
	}
	if listener.opts.PollInterval != 5*time.Minute {
		t.Errorf("PollInterval = %v", listener.opts.PollInterval)
	}
}

func TestInitBaselineOnlyOnce(t *testing.T) {
	listener, err := New(Options{Username: "u", Password: "p", OnMessage: noopHandler})
	if err != nil {
		t.Fatalf("New() 错误: %v", err)
	}

	listener.initBaseline(100)
	if listener.lastUID != 99 {
		t.Fatalf("首次水位线 = %d, want 99", listener.lastUID)
	}

	listener.lastUID = 105
	listener.initBaseline(120)
	if listener.lastUID != 105 {
		t.Fatalf("重连不应重置水位线: 得到 %d, want 105", listener.lastUID)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	cases := map[string]Options{
		"缺少用户名": {Password: "p", OnMessage: noopHandler},
		"缺少密码":  {Username: "u", OnMessage: noopHandler},
		"缺少回调":  {Username: "u", Password: "p"},
		"端口非法":  {Username: "u", Password: "p", Port: 70000, OnMessage: noopHandler},
	}
	for name, opts := range cases {
		if _, err := New(opts); err == nil {
			t.Fatalf("%s 应报错", name)
		}
	}
}
