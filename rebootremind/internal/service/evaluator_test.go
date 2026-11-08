package service

import (
	"testing"
	"time"

	"rebootremind/internal/config"
	"rebootremind/internal/model"
)

func snapshot(uptimeHours float64, memoryPercent float64) model.Snapshot {
	return model.Snapshot{
		UptimeSeconds: int64(uptimeHours * 3600),
		Memory:        model.MemoryStatus{Percent: memoryPercent},
	}
}

func TestEvaluate(t *testing.T) {
	cfg := config.Default()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	future := now.Add(4 * time.Hour)
	past := now.Add(-4 * time.Hour)

	tests := []struct {
		name        string
		snapshot    model.Snapshot
		snoozeUntil *time.Time
		wantNotify  bool
		wantReason  string
		wantTrigger string
	}{
		{
			name:        "Snooze 冷却期内一律静默",
			snapshot:    snapshot(100, 95),
			snoozeUntil: &future,
			wantNotify:  false,
			wantReason:  model.ReasonSnooze,
		},
		{
			name:        "Snooze 已过期后重新判定",
			snapshot:    snapshot(100, 95),
			snoozeUntil: &past,
			wantNotify:  true,
			wantReason:  model.ReasonUptimeAndMemory,
			wantTrigger: model.TriggerUptimeAndMemory,
		},
		{
			name:       "开机时长不足时不打扰",
			snapshot:   snapshot(10, 99),
			wantNotify: false,
			wantReason: model.ReasonUptimeBelowMinimum,
		},
		{
			name:        "开机满强制时长无条件提醒",
			snapshot:    snapshot(200, 10),
			wantNotify:  true,
			wantReason:  model.ReasonUptimeForce,
			wantTrigger: model.TriggerUptimeForce,
		},
		{
			name:        "开机与内存同时达标",
			snapshot:    snapshot(100, 90),
			wantNotify:  true,
			wantReason:  model.ReasonUptimeAndMemory,
			wantTrigger: model.TriggerUptimeAndMemory,
		},
		{
			name:       "开机达标但内存不足",
			snapshot:   snapshot(100, 50),
			wantNotify: false,
			wantReason: model.ReasonMemoryBelowMinimum,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Evaluate(test.snapshot, cfg, test.snoozeUntil, now)
			if got.ShouldNotify != test.wantNotify {
				t.Fatalf("ShouldNotify = %v, want %v", got.ShouldNotify, test.wantNotify)
			}
			if got.Reason != test.wantReason {
				t.Fatalf("Reason = %q, want %q", got.Reason, test.wantReason)
			}
			if got.Trigger != test.wantTrigger {
				t.Fatalf("Trigger = %q, want %q", got.Trigger, test.wantTrigger)
			}
		})
	}
}

func TestHealth(t *testing.T) {
	cfg := config.Default()
	if got := Health(snapshot(10, 99), cfg); got != "healthy" {
		t.Fatalf("Health = %q, want healthy", got)
	}
	if got := Health(snapshot(100, 50), cfg); got != "watch" {
		t.Fatalf("Health = %q, want watch", got)
	}
	if got := Health(snapshot(200, 10), cfg); got != "attention" {
		t.Fatalf("Health = %q, want attention", got)
	}
}
