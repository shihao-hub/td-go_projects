package outbox

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newEntry(id string, uid uint32) Entry {
	now := time.Now()
	return Entry{
		ID:          id,
		UID:         uid,
		UIDValidity: 7,
		Thread:      "repo#1",
		From:        "github@example.com",
		Subject:     "test",
		Raw:         []byte("Subject: test\r\n\r\nbody"),
		State:       StatePending,
		ReadyAt:     now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func TestPutAndReload(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() 错误: %v", err)
	}
	entry := newEntry(EntryID(7, 12), 12)
	if err := store.Put(entry); err != nil {
		t.Fatalf("Put() 错误: %v", err)
	}
	if got := store.Count(StatePending); got != 1 {
		t.Fatalf("pending 数量 = %d, want 1", got)
	}

	// 模拟进程重启：重新打开目录后 raw 与状态必须完整恢复。
	reloaded, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() 错误: %v", err)
	}
	got, ok := reloaded.Get(entry.ID)
	if !ok {
		t.Fatal("重启后记录丢失")
	}
	if got.State != StatePending || !bytes.Equal(got.Raw, entry.Raw) {
		t.Errorf("记录恢复异常: %+v", got)
	}
}

func TestPutIsIdempotentForSentEntry(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() 错误: %v", err)
	}
	entry := newEntry(EntryID(7, 12), 12)
	if err := store.Put(entry); err != nil {
		t.Fatalf("Put() 错误: %v", err)
	}
	entry.State = StateSent
	entry.Raw = nil
	if err := store.Update(entry); err != nil {
		t.Fatalf("Update() 错误: %v", err)
	}

	// 重复处理同一 UID（例如水位线未推进导致重拉）不应把已投递记录改回待投递。
	again := newEntry(EntryID(7, 12), 12)
	if err := store.Put(again); err != nil {
		t.Fatalf("Put() 错误: %v", err)
	}
	if got, _ := store.Get(entry.ID); got.State != StateSent {
		t.Fatalf("已投递记录被覆盖为 %q", got.State)
	}
}

func TestRecoverExpiredLeases(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() 错误: %v", err)
	}
	now := time.Now()
	expired := newEntry(EntryID(7, 1), 1)
	expired.State = StateSending
	expired.LeaseUntil = now.Add(-time.Second)
	active := newEntry(EntryID(7, 2), 2)
	active.State = StateSending
	active.LeaseUntil = now.Add(time.Hour)
	for _, entry := range []Entry{expired, active} {
		if err := store.Put(entry); err != nil {
			t.Fatalf("Put() 错误: %v", err)
		}
	}

	if recovered := store.RecoverExpiredLeases(now, time.Minute); recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	if got, _ := store.Get(expired.ID); got.State != StatePending {
		t.Errorf("过期租约应恢复为 pending，got %q", got.State)
	}
	if got, _ := store.Get(active.ID); got.State != StateSending {
		t.Errorf("有效租约不应被恢复，got %q", got.State)
	}
}

func TestLastSentAndGC(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() 错误: %v", err)
	}
	now := time.Now()
	sent := newEntry(EntryID(7, 1), 1)
	sent.State = StateSent
	sent.Raw = nil
	sent.UpdatedAt = now.Add(-time.Hour)
	if err := store.Put(sent); err != nil {
		t.Fatalf("Put() 错误: %v", err)
	}
	if got := store.LastSent("repo#1"); got.IsZero() {
		t.Error("LastSent 应返回成功投递时间")
	}
	if got := store.LastSent("other"); !got.IsZero() {
		t.Errorf("其他讨论串不应命中，got %v", got)
	}

	if removed := store.GC(now, 30*time.Minute); removed != 1 {
		t.Fatalf("GC 应清理 1 条过期记录，got %d", removed)
	}
	if store.Count("") != 0 {
		t.Errorf("清理后仍有记录: %+v", store.List())
	}
	if _, err := os.Stat(filepath.Join(dir, sent.ID+fileSuffix)); !os.IsNotExist(err) {
		t.Errorf("过期记录文件应被删除，stat err = %v", err)
	}
}

func TestUpdateMissingEntryFails(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() 错误: %v", err)
	}
	if err := store.Update(newEntry("missing", 1)); err == nil {
		t.Fatal("更新不存在的记录应报错")
	}
}

func TestOpenSkipsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("写入损坏文件失败: %v", err)
	}
	store, err := Open(dir)
	if err == nil {
		t.Fatal("存在损坏文件时应返回错误供调用方告警")
	}
	if store == nil {
		t.Fatal("损坏文件不应让整个队列不可用")
	}
	entry := newEntry(EntryID(7, 3), 3)
	if err := store.Put(entry); err != nil {
		t.Fatalf("损坏文件存在时仍应可写入: %v", err)
	}
}
