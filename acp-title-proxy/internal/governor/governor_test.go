package governor

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type testLock struct {
	mu sync.Mutex
}

func (l *testLock) Lock() error {
	l.mu.Lock()
	return nil
}

func (l *testLock) Unlock() {
	l.mu.Unlock()
}

func TestGovernorAcquireAndLimit(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "sessions.json")
	lock := &testLock{}

	aliveMap := map[int]bool{
		1001: true,
		1002: true,
		1003: true,
	}
	var killedPID int

	isAlive := func(pid int) bool {
		if pid == os.Getpid() {
			return true
		}
		return aliveMap[pid]
	}
	killTree := func(pid int) error {
		killedPID = pid
		delete(aliveMap, pid)
		return nil
	}

	// 预先写入 3 个 session
	govInit := NewGovernorWithConfig(3, storePath, lock, isAlive, killTree)
	now := time.Now()
	govInit.saveLocked([]SessionInfo{
		{PID: 1001, State: StateIdle, CreatedAt: now.Add(-30 * time.Minute), LastActiveAt: now.Add(-20 * time.Minute)},
		{PID: 1002, State: StateIdle, CreatedAt: now.Add(-20 * time.Minute), LastActiveAt: now.Add(-10 * time.Minute)},
		{PID: 1003, State: StateBusy, CreatedAt: now.Add(-10 * time.Minute), LastActiveAt: now.Add(-1 * time.Minute)},
	})

	// 启动第 4 个会话（PID 为当前进程自身）
	gov4 := NewGovernorWithConfig(3, storePath, lock, isAlive, killTree)
	if err := gov4.Acquire(); err != nil {
		t.Fatalf("expected Acquire to succeed by evicting oldest idle session, got: %v", err)
	}

	// 1001 是最旧的 Idle 会话，应该被淘汰
	if killedPID != 1001 {
		t.Fatalf("expected 1001 to be killed, got: %d", killedPID)
	}

	sessions := gov4.List()
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}

	// 验证 1001 不在列表中
	for _, s := range sessions {
		if s.PID == 1001 {
			t.Fatalf("expected 1001 to be evicted")
		}
	}
}

func TestGovernorAllBusyReject(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "sessions.json")
	lock := &testLock{}

	aliveMap := map[int]bool{
		2001: true,
		2002: true,
		2003: true,
	}

	isAlive := func(pid int) bool {
		if pid == os.Getpid() {
			return true
		}
		return aliveMap[pid]
	}
	killTree := func(pid int) error {
		return nil
	}

	// 3 个均在运行中 (StateBusy)
	govInit := NewGovernorWithConfig(3, storePath, lock, isAlive, killTree)
	now := time.Now()
	govInit.saveLocked([]SessionInfo{
		{PID: 2001, State: StateBusy, CreatedAt: now, LastActiveAt: now},
		{PID: 2002, State: StateBusy, CreatedAt: now, LastActiveAt: now},
		{PID: 2003, State: StateBusy, CreatedAt: now, LastActiveAt: now},
	})

	gov4 := NewGovernorWithConfig(3, storePath, lock, isAlive, killTree)
	err := gov4.Acquire()
	if err == nil {
		t.Fatalf("expected error when all sessions are busy, got nil")
	}
}

func TestGovernorDeadSessionCleanup(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "sessions.json")
	lock := &testLock{}

	aliveMap := map[int]bool{
		3001: false, // 已经死掉
		3002: true,  // 活着
	}

	isAlive := func(pid int) bool {
		if pid == os.Getpid() {
			return true
		}
		return aliveMap[pid]
	}
	killTree := func(pid int) error {
		return nil
	}

	govInit := NewGovernorWithConfig(3, storePath, lock, isAlive, killTree)
	now := time.Now()
	govInit.saveLocked([]SessionInfo{
		{PID: 3001, State: StateIdle, CreatedAt: now, LastActiveAt: now},
		{PID: 3002, State: StateIdle, CreatedAt: now, LastActiveAt: now},
	})

	govSelf := NewGovernorWithConfig(3, storePath, lock, isAlive, killTree)
	if err := govSelf.Acquire(); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	sessions := govSelf.List()
	// 3001 已死被清理，剩余 3002 + 当前自身 = 2 个
	if len(sessions) != 2 {
		t.Fatalf("expected 2 alive sessions, got %d", len(sessions))
	}
	for _, s := range sessions {
		if s.PID == 3001 {
			t.Fatalf("dead session 3001 was not cleaned up")
		}
	}
}

func TestGovernorStateTransitions(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "sessions.json")
	lock := &testLock{}

	isAlive := func(pid int) bool {
		return true
	}
	killTree := func(pid int) error {
		return nil
	}

	gov := NewGovernorWithConfig(3, storePath, lock, isAlive, killTree)
	if err := gov.Acquire(); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// 初始状态为 idle
	list := gov.List()
	if len(list) != 1 || list[0].State != StateIdle {
		t.Fatalf("expected 1 idle session, got %+v", list)
	}

	// 接收 prompt -> busy
	gov.OnPromptStart()
	list = gov.List()
	if list[0].State != StateBusy {
		t.Fatalf("expected busy state, got %s", list[0].State)
	}

	// 关联 session ID
	gov.OnSessionID("test-session-123")
	list = gov.List()
	if list[0].SessionID != "test-session-123" {
		t.Fatalf("expected session ID test-session-123, got %s", list[0].SessionID)
	}

	// prompt 完成 -> idle
	gov.OnPromptDone()
	list = gov.List()
	if list[0].State != StateIdle {
		t.Fatalf("expected idle state, got %s", list[0].State)
	}

	// 退出 -> 列表清空
	gov.Release()
	list = gov.List()
	if len(list) != 0 {
		t.Fatalf("expected 0 sessions after release, got %d", len(list))
	}
}
