package service

import (
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)

	state := State{}
	Snooze(&state, now, 4*time.Hour, ActionSnoozeShort)
	state.LastNotifiedAt = &now
	if err := SaveState(dir, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.SnoozeUntil == nil || !loaded.SnoozeUntil.Equal(now.Add(4*time.Hour)) {
		t.Fatalf("SnoozeUntil = %v", loaded.SnoozeUntil)
	}
	if loaded.LastAction != ActionSnoozeShort {
		t.Fatalf("LastAction = %q", loaded.LastAction)
	}
	if loaded.LastNotifiedAt == nil || !loaded.LastNotifiedAt.Equal(now) {
		t.Fatalf("LastNotifiedAt = %v", loaded.LastNotifiedAt)
	}
	if loaded.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt 不应为零值")
	}
}

func TestLoadStateMissingFile(t *testing.T) {
	state, err := LoadState(t.TempDir())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.SnoozeUntil != nil || state.LastAction != "" {
		t.Fatalf("文件缺失时应返回零值: %+v", state)
	}
}
