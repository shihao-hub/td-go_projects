// Package service 承载巡检判定与延迟提醒状态等业务规则。
package service

import (
	"time"

	"rebootremind/internal/config"
	"rebootremind/internal/model"
)

// Evaluate 依据开机时长硬前置、内存水位与 Snooze 状态给出判定。
//
// 规则顺序（不可调换）：
//  1. Snooze 冷却期内一律不打扰；
//  2. 开机时长未达下限一律不打扰，杜绝刚开机高负载误报；
//  3. 开机达到强制保养时长则无条件提醒；
//  4. 开机达标且内存达到水位才提醒。
func Evaluate(snapshot model.Snapshot, cfg config.Config, snoozeUntil *time.Time, now time.Time) model.Decision {
	if snoozeUntil != nil && now.Before(*snoozeUntil) {
		return model.Decision{ShouldNotify: false, Reason: model.ReasonSnooze}
	}

	uptimeHours := float64(snapshot.UptimeSeconds) / 3600
	if uptimeHours < cfg.MinUptimeHours {
		return model.Decision{ShouldNotify: false, Reason: model.ReasonUptimeBelowMinimum}
	}
	if uptimeHours >= cfg.ForceUptimeHours {
		return model.Decision{
			ShouldNotify: true,
			Trigger:      model.TriggerUptimeForce,
			Reason:       model.ReasonUptimeForce,
		}
	}
	if snapshot.Memory.Percent >= cfg.MinMemoryPercent {
		return model.Decision{
			ShouldNotify: true,
			Trigger:      model.TriggerUptimeAndMemory,
			Reason:       model.ReasonUptimeAndMemory,
		}
	}
	return model.Decision{ShouldNotify: false, Reason: model.ReasonMemoryBelowMinimum}
}

// Health 给出面向用户的健康等级：healthy / watch / attention。
func Health(snapshot model.Snapshot, cfg config.Config) string {
	uptimeHours := float64(snapshot.UptimeSeconds) / 3600
	if uptimeHours < cfg.MinUptimeHours {
		return "healthy"
	}
	if uptimeHours >= cfg.ForceUptimeHours || snapshot.Memory.Percent >= cfg.MinMemoryPercent {
		return "attention"
	}
	return "watch"
}
