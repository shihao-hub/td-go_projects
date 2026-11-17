package imap

import (
	"testing"
	"time"

	imapv1 "github.com/emersion/go-imap"
)

func noopHandler(uint32, uint32, []byte) error { return nil }

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

type fakeState struct {
	username    string
	uidValidity uint32
	lastUID     uint32
	ok          bool
	saves       []uint32
}

func (f *fakeState) LastUID(username string, uidValidity uint32) (uint32, bool) {
	if f.ok && f.username == username && f.uidValidity == uidValidity {
		return f.lastUID, true
	}
	return 0, false
}

func (f *fakeState) Save(username string, uidValidity, lastUID uint32) error {
	f.username, f.uidValidity, f.lastUID, f.ok = username, uidValidity, lastUID, true
	f.saves = append(f.saves, lastUID)
	return nil
}

func TestInitBaselineUsesPersistedState(t *testing.T) {
	store := &fakeState{username: "u", uidValidity: 7, lastUID: 120, ok: true}
	listener, err := New(Options{
		Username:  "u",
		Password:  "p",
		OnMessage: noopHandler,
		State:     store,
	})
	if err != nil {
		t.Fatalf("New() 错误: %v", err)
	}
	listener.uidValidity = 7
	listener.initBaseline(500)

	if listener.lastUID != 120 {
		t.Fatalf("应沿用持久化水位线 120，got %d", listener.lastUID)
	}
	if len(store.saves) != 0 {
		t.Fatalf("沿用已有水位线时不应重写，saves=%v", store.saves)
	}
}

func TestInitBaselineResetsWhenUIDValidityChanges(t *testing.T) {
	store := &fakeState{username: "u", uidValidity: 6, lastUID: 120, ok: true}
	listener, err := New(Options{
		Username:  "u",
		Password:  "p",
		OnMessage: noopHandler,
		State:     store,
	})
	if err != nil {
		t.Fatalf("New() 错误: %v", err)
	}
	listener.uidValidity = 7
	listener.initBaseline(500)

	if listener.lastUID != 499 {
		t.Fatalf("UIDVALIDITY 变化后应以 UIDNEXT-1=499 重建，got %d", listener.lastUID)
	}
	if len(store.saves) != 1 || store.saves[0] != 499 {
		t.Fatalf("新基线应落盘，saves=%v", store.saves)
	}
}

// TestSyncMailboxRebuildsOnRuntimeUIDValidityChange 覆盖「进程运行期间邮箱被重建」：
// 旧水位线对新邮箱没有意义，必须按新 UIDNEXT 重建，否则新邮件会被跳过。
func TestSyncMailboxRebuildsOnRuntimeUIDValidityChange(t *testing.T) {
	listener, err := New(Options{Username: "u", Password: "p", OnMessage: noopHandler})
	if err != nil {
		t.Fatalf("New() 错误: %v", err)
	}
	listener.uidValidity = 7
	listener.lastUID = 120
	listener.baselineSet = true

	listener.syncMailbox(&imapv1.MailboxStatus{UidValidity: 8, UidNext: 500})

	if listener.uidValidity != 8 {
		t.Errorf("uidValidity = %d, want 8", listener.uidValidity)
	}
	if listener.lastUID != 499 {
		t.Errorf("UIDVALIDITY 变化后应以 UIDNEXT-1=499 重建，got %d", listener.lastUID)
	}
}

// TestSyncMailboxKeepsBaselineWithoutChange 确认普通重连不会重置水位线，
// 断线期间到达的邮件仍会被补拉。
func TestSyncMailboxKeepsBaselineWithoutChange(t *testing.T) {
	listener, err := New(Options{Username: "u", Password: "p", OnMessage: noopHandler})
	if err != nil {
		t.Fatalf("New() 错误: %v", err)
	}
	listener.uidValidity = 7
	listener.lastUID = 120
	listener.baselineSet = true

	listener.syncMailbox(&imapv1.MailboxStatus{UidValidity: 7, UidNext: 500})

	if listener.lastUID != 120 {
		t.Errorf("同一 UIDVALIDITY 下不应重置水位线，got %d", listener.lastUID)
	}
}

func TestPersistLastUIDWithoutStore(t *testing.T) {
	listener, err := New(Options{Username: "u", Password: "p", OnMessage: noopHandler})
	if err != nil {
		t.Fatalf("New() 错误: %v", err)
	}
	listener.lastUID = 10
	listener.persistLastUID() // 未配置 State 时不应 panic
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
