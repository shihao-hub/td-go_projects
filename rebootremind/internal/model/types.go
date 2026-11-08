// Package model 定义公开数据契约（JSON 字段即 CLI --json 输出）。
package model

import "time"

// ProcessUsage 是单个进程名的内存占用聚合结果。
type ProcessUsage struct {
	Name            string  `json:"name"`
	WorkingSetBytes uint64  `json:"working_set_bytes"`
	Percent         float64 `json:"percent"`
}

// MemoryStatus 是整机物理内存使用情况。
type MemoryStatus struct {
	TotalBytes uint64  `json:"total_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	Percent    float64 `json:"percent"`
}

// Snapshot 是一次系统健康快照。
type Snapshot struct {
	UptimeSeconds int64          `json:"uptime_seconds"`
	Memory        MemoryStatus   `json:"memory"`
	TopProcesses  []ProcessUsage `json:"top_processes"`
}

// Thresholds 是当前生效的提醒阈值。
type Thresholds struct {
	MinUptimeHours   float64 `json:"min_uptime_hours"`
	MinMemoryPercent float64 `json:"min_memory_percent"`
	ForceUptimeHours float64 `json:"force_uptime_hours"`
}

// 判定触发来源。
const (
	TriggerNone            = ""
	TriggerUptimeAndMemory = "uptime_and_memory"
	TriggerUptimeForce     = "uptime_force"
	TriggerForced          = "forced"
)

// 判定原因码。
const (
	ReasonSnooze             = "snooze"
	ReasonUptimeBelowMinimum = "uptime_below_minimum"
	ReasonMemoryBelowMinimum = "memory_below_threshold"
	ReasonUptimeForce        = "uptime_force"
	ReasonUptimeAndMemory    = "uptime_and_memory"
	ReasonForced             = "forced"
)

// Decision 是巡检判定结果。
type Decision struct {
	ShouldNotify bool   `json:"should_notify"`
	Trigger      string `json:"trigger,omitempty"`
	Reason       string `json:"reason"`
}

// Status 是 status 命令的公开结果。
type Status struct {
	Snapshot
	Thresholds   Thresholds `json:"thresholds"`
	SnoozeUntil  *time.Time `json:"snooze_until,omitempty"`
	SnoozeActive bool       `json:"snooze_active"`
	WouldNotify  bool       `json:"would_notify"`
	Health       string     `json:"health"`
}

// CheckResult 是 check 命令的公开结果。
type CheckResult struct {
	Notified         bool       `json:"notified"`
	Forced           bool       `json:"forced"`
	Action           string     `json:"action,omitempty"`
	RestartRequested bool       `json:"restart_requested"`
	Decision         Decision   `json:"decision"`
	Snapshot         Snapshot   `json:"snapshot"`
	SnoozeUntil      *time.Time `json:"snooze_until,omitempty"`
}

// TaskStatus 是 task status 命令的公开结果。
type TaskStatus struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	ExePath   string `json:"exe_path"`
	NextRun   string `json:"next_run,omitempty"`
}
