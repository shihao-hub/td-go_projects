// Package state 持久化 IMAP UID 水位线，保证进程重启后从上次位置继续拉取。
//
// 水位线只在同一账号、同一 UIDVALIDITY 下有效；任一不匹配都视为无效，
// 由调用方重新建立基线，避免把旧账号或重建邮箱的 UID 当成自己的。
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	fileName      = "state.json"
	schemaVersion = 1
)

// Snapshot 是落盘的运行状态。
type Snapshot struct {
	Version     int       `json:"version"`
	Username    string    `json:"username"`
	UIDValidity uint32    `json:"uid_validity"`
	LastUID     uint32    `json:"last_uid"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store 是线程安全的 JSON 状态文件存储。
type Store struct {
	path string

	mu   sync.Mutex
	snap Snapshot
	has  bool
}

// New 创建指向指定路径的存储。
func New(path string) *Store {
	return &Store{path: path}
}

// Open 读取已有状态。文件不存在时返回空存储；文件损坏时返回错误，
// 但返回的 Store 仍可正常 Save，调用方只需记录告警。
func Open(path string) (*Store, error) {
	store := New(path)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, fmt.Errorf("读取状态文件失败: %w", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return store, fmt.Errorf("解析状态文件失败: %w", err)
	}
	store.snap = snap
	store.has = true
	return store, nil
}

// LastUID 返回指定账号与 UIDVALIDITY 下的水位线。
func (s *Store) LastUID(username string, uidValidity uint32) (uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.has || s.snap.Username != username || s.snap.UIDValidity != uidValidity || s.snap.LastUID == 0 {
		return 0, false
	}
	return s.snap.LastUID, true
}

// Save 原子写入水位线：先写临时文件再替换，避免断电留下半个 JSON。
func (s *Store) Save(username string, uidValidity, lastUID uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := Snapshot{
		Version:     schemaVersion,
		Username:    username,
		UIDValidity: uidValidity,
		LastUID:     lastUID,
		UpdatedAt:   time.Now().UTC(),
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建状态目录失败: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入临时状态文件失败: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换状态文件失败: %w", err)
	}

	s.snap = snap
	s.has = true
	return nil
}

// Snapshot 返回当前内存状态的副本。
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}
