package engine

import (
	"testing"
	"time"

	"file-sync-native/models"
)

func TestSpeedWindowAndETA(t *testing.T) {
	tr := NewTracker("t")
	tr.CopyEnqueued(10 << 20) // 10 MiB

	now := time.Now()
	tr.mu.Lock()
	tr.samples = []speedSample{
		{t: now.Add(-8 * time.Second), done: 0},
	}
	tr.mu.Unlock()

	tr.mu.Lock()
	tr.p.DoneBytes = 8 << 20
	tr.samples = append(tr.samples, speedSample{t: now, done: 8 << 20})
	tr.recomputeSpeedLocked(now)
	tr.mu.Unlock()

	p := tr.Snapshot()
	if p.SpeedBPS != 1<<20 {
		t.Fatalf("speed: want %d bps over 8s for 8MiB, got %d", 1<<20, p.SpeedBPS)
	}
	if p.ETASeconds != 2 {
		t.Fatalf("eta: want 2s for 2MiB remaining at 1MiB/s, got %d", p.ETASeconds)
	}
}

func TestETAHidesWhenSpanTooShort(t *testing.T) {
	tr := NewTracker("t")
	tr.CopyEnqueued(1 << 20)
	now := time.Now()
	tr.mu.Lock()
	tr.p.DoneBytes = 1 << 19
	tr.samples = []speedSample{{t: now.Add(-1 * time.Second), done: 0}}
	tr.recomputeSpeedLocked(now)
	tr.mu.Unlock()
	if p := tr.Snapshot(); p.ETASeconds != 0 {
		t.Fatalf("eta should be hidden for <3s span, got %d", p.ETASeconds)
	}
	if p := tr.Snapshot(); p.SpeedBPS <= 0 {
		t.Fatal("speed should still be reported")
	}
}

func TestDeleteTickProgress(t *testing.T) {
	tr := NewTracker("t")
	tr.DeleteTick(1, 4, "a.txt")
	p := tr.Snapshot()
	if p.Status != models.StatusDeleting || p.Percentage != 25 {
		t.Fatalf("delete progress: %+v", p)
	}
}

func TestSnapshotIsolatesPendingDeletes(t *testing.T) {
	tr := NewTracker("t")
	list := []string{"a", "b"}
	tr.AwaitingDelete(list)
	s1 := tr.Snapshot()
	s1.PendingDeletes[0] = "MUTATED"
	s2 := tr.Snapshot()
	if s2.PendingDeletes[0] != "a" {
		t.Fatal("snapshot must deep-copy PendingDeletes")
	}
}
