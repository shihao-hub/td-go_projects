package engine

import (
	"sync"
	"time"

	"file-sync-native/models"
)

const (
	speedWindow = 10 * time.Second // 速率滑动窗口宽度
	minETASpan  = 3 * time.Second  // 窗口跨度不足此值时不上报 ETA
)

type speedSample struct {
	t    time.Time
	done int64 // 累计已复制字节
}

// Tracker 维护同步进度的并发安全快照，供 UI 层轮询/订阅。
type Tracker struct {
	mu      sync.Mutex
	p       models.SyncProgress
	samples []speedSample
	notify  func(models.SyncProgress)
}

func NewTracker(taskID string) *Tracker {
	return &Tracker{p: models.SyncProgress{TaskID: taskID, Status: models.StatusPending}}
}

// SetNotify 注册状态变化回调。回调在锁外调用，允许回调内再读快照。
func (t *Tracker) SetNotify(fn func(models.SyncProgress)) {
	t.mu.Lock()
	t.notify = fn
	t.mu.Unlock()
}

func (t *Tracker) Snapshot() models.SyncProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	return cloneProgress(t.p)
}

func cloneProgress(p models.SyncProgress) models.SyncProgress {
	c := p
	c.PendingDeletes = append([]string(nil), p.PendingDeletes...)
	return c
}

func (t *Tracker) emitLocked() {
	snap := cloneProgress(t.p)
	n := t.notify
	t.mu.Unlock()
	if n != nil {
		n(snap)
	}
}

func (t *Tracker) SetStatus(st string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.p.Status == st {
		return
	}
	t.p.Status = st
}

// ScanTick 在源扫描每发现一个文件时调用（总数未知，只有计数）。
func (t *Tracker) ScanTick(rel string) {
	t.mu.Lock()
	t.p.Status = models.StatusScanning
	t.p.ScannedFiles++
	t.p.CurrentPath = rel
	t.emitLocked()
}

// CopyEnqueued 在产生一个复制动作时调用，累加待复制总量。
func (t *Tracker) CopyEnqueued(size int64) {
	t.mu.Lock()
	t.p.TotalFiles++
	t.p.TotalBytes += size
	t.emitLocked()
}

// FileDone 在一个文件复制完成（含失败计为已处理）时调用，
// 同时推进滑动窗口速率与 ETA。
func (t *Tracker) FileDone(size int64, rel string) {
	t.mu.Lock()
	t.p.DoneFiles++
	t.p.DoneBytes += size
	t.p.CurrentPath = rel
	now := time.Now()
	t.samples = append(t.samples, speedSample{t: now, done: t.p.DoneBytes})
	t.recomputeSpeedLocked(now)
	if t.p.TotalBytes > 0 {
		t.p.Percentage = float64(t.p.DoneBytes) / float64(t.p.TotalBytes) * 100
	}
	t.emitLocked()
}

func (t *Tracker) recomputeSpeedLocked(now time.Time) {
	cutoff := now.Add(-speedWindow)
	start := -1
	for i, s := range t.samples {
		if !s.t.Before(cutoff) {
			start = i
			break
		}
	}
	if start < 0 {
		start = len(t.samples) - 1
	}
	if start < 0 {
		return
	}
	t.samples = t.samples[start:]
	first := t.samples[0]
	span := now.Sub(first.t)
	t.p.SpeedBPS = 0
	t.p.ETASeconds = 0
	if span <= 0 {
		return
	}
	speed := float64(t.p.DoneBytes-first.done) / span.Seconds()
	t.p.SpeedBPS = int64(speed)
	if span >= minETASpan && speed > 1024 && t.p.TotalBytes > t.p.DoneBytes {
		t.p.ETASeconds = int(float64(t.p.TotalBytes-t.p.DoneBytes) / speed)
	}
}

// CopyPhase 在扫描结束、复制队列仍在消化时切换状态。
func (t *Tracker) CopyPhase() {
	t.mu.Lock()
	t.p.Status = models.StatusCopying
	t.emitLocked()
}

// maxPendingDeletesInSnapshot 限制快照里携带的待删清单长度，
// 避免海量删除时事件负载过大；完整清单保留在 engine 侧用于执行。
const maxPendingDeletesInSnapshot = 500

// AwaitingDelete 请求用户确认删除清单。
func (t *Tracker) AwaitingDelete(deletes []string) {
	t.mu.Lock()
	t.p.Status = models.StatusAwaitingDelete
	list := deletes
	if len(list) > maxPendingDeletesInSnapshot {
		list = list[:maxPendingDeletesInSnapshot]
	}
	t.p.PendingDeletes = append([]string(nil), list...)
	t.p.Percentage = 100
	t.emitLocked()
}

// DeleteTick 删除阶段进度（条目维度，复用 Done/TotalFiles 与 Percentage）。
func (t *Tracker) DeleteTick(done, total int, rel string) {
	t.mu.Lock()
	t.p.Status = models.StatusDeleting
	t.p.DoneFiles = done
	t.p.TotalFiles = total
	t.p.CurrentPath = rel
	if total > 0 {
		t.p.Percentage = float64(done) / float64(total) * 100
	}
	t.emitLocked()
}

func (t *Tracker) Complete() {
	t.mu.Lock()
	if t.p.Status == models.StatusError || t.p.Status == models.StatusCancelled {
		t.emitLocked()
		return
	}
	t.p.Status = models.StatusCompleted
	t.p.Percentage = 100
	t.p.CurrentPath = ""
	t.emitLocked()
}

func (t *Tracker) Fail(msg string) {
	t.mu.Lock()
	t.p.Status = models.StatusError
	t.p.ErrorMessage = msg
	t.emitLocked()
}

func (t *Tracker) Cancelled() {
	t.mu.Lock()
	t.p.Status = models.StatusCancelled
	t.emitLocked()
}
