package pool

import (
	"testing"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/config"
)

func TestFilterCandidatesPure_SingleProcessExempt(t *testing.T) {
	now := time.Now()
	// 只有一个实例时，即使存活 100 小时也绝对豁免
	list := []HarnessInfo{
		{
			PID:       1001,
			ParentPID: 500,
			CreatedAt: now.Add(-100 * time.Hour),
		},
	}

	candidates := FilterCandidatesPure(list, 20*time.Minute, now)
	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates for single process, got %d", len(candidates))
	}
}

func TestFilterCandidatesPure_NewestAlwaysExempt(t *testing.T) {
	now := time.Now()
	// 有 3 个实例：1 个凌晨创建（超期），1 个 1 小时前创建（超期），1 个 10 分钟前创建（最新）
	list := []HarnessInfo{
		{PID: 1001, ParentPID: 500, CreatedAt: now.Add(-5 * time.Hour)},
		{PID: 1002, ParentPID: 500, CreatedAt: now.Add(-60 * time.Minute)},
		{PID: 1003, ParentPID: 500, CreatedAt: now.Add(-10 * time.Minute)},
	}

	candidates := FilterCandidatesPure(list, 20*time.Minute, now)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	if candidates[0].PID != 1001 || candidates[1].PID != 1002 {
		t.Fatalf("unexpected candidates order/PIDs: %+v", candidates)
	}
}

func TestFilterCandidatesPure_UnsortedInput(t *testing.T) {
	now := time.Now()
	// 无序输入，必须正确按 CreatedAt 排序并豁免最新的一项
	list := []HarnessInfo{
		{PID: 3003, ParentPID: 500, CreatedAt: now.Add(-2 * time.Minute)},  // 最新
		{PID: 3001, ParentPID: 500, CreatedAt: now.Add(-50 * time.Minute)}, // 超期
		{PID: 3002, ParentPID: 500, CreatedAt: now.Add(-40 * time.Minute)}, // 超期
	}

	candidates := FilterCandidatesPure(list, 20*time.Minute, now)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	if candidates[0].PID != 3001 || candidates[1].PID != 3002 {
		t.Fatalf("expected 3001 and 3002, got %+v", candidates)
	}
}

func TestReaperConfigDefaults(t *testing.T) {
	cfg := config.DefaultConfig()
	if !cfg.Reaper.Enabled {
		t.Fatalf("expected reaper to be enabled by default")
	}
	if cfg.Reaper.IdleMinutes != 20 {
		t.Fatalf("expected default idle_minutes to be 20, got %d", cfg.Reaper.IdleMinutes)
	}
	if cfg.Reaper.CheckIntervalMinutes != 5 {
		t.Fatalf("expected default check_interval_minutes to be 5, got %d", cfg.Reaper.CheckIntervalMinutes)
	}
}
