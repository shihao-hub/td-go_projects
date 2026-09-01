package sync

import (
	stdsync "sync"

	"file-sync/models"
)

type ProgressTracker struct {
	mu   stdsync.Mutex
	p    models.SyncProgress
	subs map[chan models.SyncProgress]struct{}
	done bool
}

func NewProgressTracker(taskID string) *ProgressTracker {
	return &ProgressTracker{
		p:    models.SyncProgress{TaskID: taskID, Status: "pending"},
		subs: make(map[chan models.SyncProgress]struct{}),
	}
}

func (t *ProgressTracker) broadcastLocked() {
	snap := t.p
	for ch := range t.subs {
		select {
		case ch <- snap:
		default:
		}
	}
}

func (t *ProgressTracker) Snapshot() models.SyncProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.p
}

func (t *ProgressTracker) SetStatus(st string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.p.Status = st
	t.broadcastLocked()
}

func (t *ProgressTracker) SetScanProgress(count int, cur string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.p.Status = "scanning"
	t.p.CurrentFile = count
	t.p.TotalFiles = 0
	t.p.Percentage = 0
	t.p.CurrentPath = cur
	t.broadcastLocked()
}

func (t *ProgressTracker) StartSync(total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.p.Status = "syncing"
	t.p.TotalFiles = total
	t.p.CurrentFile = 0
	t.p.Percentage = 0
	t.broadcastLocked()
}

func (t *ProgressTracker) SetProgress(cur, total int, path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.p.Status = "syncing"
	t.p.CurrentFile = cur
	t.p.TotalFiles = total
	t.p.CurrentPath = path
	if total > 0 {
		t.p.Percentage = float64(cur) / float64(total) * 100
	} else {
		t.p.Percentage = 100
	}
	t.broadcastLocked()
}

func (t *ProgressTracker) Complete() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.p.Status = "completed"
	t.p.Percentage = 100
	t.p.CurrentPath = ""
	t.broadcastLocked()
	t.closeLocked()
}

func (t *ProgressTracker) Fail(msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.p.Status = "error"
	t.p.ErrorMessage = msg
	t.broadcastLocked()
	t.closeLocked()
}

func (t *ProgressTracker) closeLocked() {
	t.done = true
	for ch := range t.subs {
		close(ch)
	}
	t.subs = make(map[chan models.SyncProgress]struct{})
}

func (t *ProgressTracker) Subscribe() (<-chan models.SyncProgress, func()) {
	ch := make(chan models.SyncProgress, 64)
	t.mu.Lock()
	if t.done {
		close(ch)
		t.mu.Unlock()
		return ch, func() {}
	}
	t.subs[ch] = struct{}{}
	t.mu.Unlock()
	unsub := func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if _, ok := t.subs[ch]; ok {
			delete(t.subs, ch)
			close(ch)
		}
	}
	return ch, unsub
}
