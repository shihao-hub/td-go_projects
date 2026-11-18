// Package store 负责额度快照的本地缓存（不含任何凭据）。
package store

import (
	"encoding/json"
	"os"
	"time"

	"ccquota/internal/appenv"
	"ccquota/internal/model"
)

// Snapshot 是一次聚合结果的落盘形态。
type Snapshot struct {
	UpdatedAt time.Time    `json:"updatedAt"`
	Cards     []model.Card `json:"cards"`
}

// Load 读取快照；文件缺失或损坏时返回 false。
func Load() (Snapshot, bool) {
	path, err := appenv.CachePath()
	if err != nil {
		return Snapshot{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, false
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, false
	}
	return snap, true
}

// Save 覆盖写入快照。
func Save(snap Snapshot) error {
	path, err := appenv.CachePath()
	if err != nil {
		return err
	}
	payload, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(path, payload, 0o644)
}

// Fresh 判断快照是否仍在有效期内。
func Fresh(snap Snapshot, ttl time.Duration) bool {
	if snap.UpdatedAt.IsZero() || ttl <= 0 {
		return false
	}
	return time.Since(snap.UpdatedAt) < ttl
}

// ByProvider 以 providerId 建立索引，供失败时回退旧值。
func ByProvider(snap Snapshot) map[string]model.Card {
	out := make(map[string]model.Card, len(snap.Cards))
	for _, c := range snap.Cards {
		out[c.ProviderID] = c
	}
	return out
}
