// Package outbox 持久化待投递邮件，保证进程退出、重启或投递失败后仍能补投。
//
// 语义：
//   - 每条记录一个 JSON 文件，写入走「临时文件 + rename」，断电不会留下半个文件；
//   - pending：等待防抖窗口结束；sending：已被投递协程领取（带租约）；
//   - sent：投递成功，保留 raw 以外的元信息供冷却期与去重判断；
//   - failed：重试耗尽；dropped：因冷却被有意丢弃；
//   - sending 租约过期说明进程在投递途中退出，恢复为 pending 重新投递。
//
// 本包只负责存取，不决定「何时投递」，调度逻辑位于 internal/service。
package outbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 记录状态。
const (
	StatePending = "pending"
	StateSending = "sending"
	StateSent    = "sent"
	StateFailed  = "failed"
	StateDropped = "dropped"
)

const fileSuffix = ".json"

// Entry 是一封待投递邮件的持久化记录。
type Entry struct {
	ID          string `json:"id"`
	UID         uint32 `json:"uid"`
	UIDValidity uint32 `json:"uid_validity"`
	Thread      string `json:"thread"`
	From        string `json:"from,omitempty"`
	Subject     string `json:"subject,omitempty"`
	// Raw 是原始 RFC 5322 邮件；投递时重新解析，保证解析逻辑升级后仍然生效。
	Raw        []byte    `json:"raw,omitempty"`
	State      string    `json:"state"`
	Attempts   int       `json:"attempts"`
	ReadyAt    time.Time `json:"ready_at"`
	LeaseUntil time.Time `json:"lease_until,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// EntryID 生成稳定标识：同一 UIDVALIDITY 下的同一 UID 只会有一条记录。
func EntryID(uidValidity, uid uint32) string {
	return fmt.Sprintf("%d-%d", uidValidity, uid)
}

// Store 是 outbox 目录的线程安全视图。
type Store struct {
	dir string

	mu      sync.Mutex
	entries map[string]Entry
}

// Open 打开（或创建）目录并载入全部记录。
// 目录不可用时返回错误；存在损坏文件时仍返回可用的 Store 与错误，调用方只需告警。
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建待投递目录失败: %w", err)
	}
	store := &Store{dir: dir, entries: make(map[string]Entry)}
	return store, store.load()
}

func (s *Store) load() error {
	items, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("读取待投递目录失败: %w", err)
	}
	var problems []string
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), fileSuffix) {
			continue
		}
		path := filepath.Join(s.dir, item.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", item.Name(), readErr))
			continue
		}
		var entry Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", item.Name(), err))
			continue
		}
		if entry.ID == "" {
			entry.ID = strings.TrimSuffix(item.Name(), fileSuffix)
		}
		s.entries[entry.ID] = entry
	}
	if len(problems) > 0 {
		return fmt.Errorf("跳过 %d 个损坏的待投递记录: %s", len(problems), strings.Join(problems, "; "))
	}
	return nil
}

// List 返回全部记录的副本（按 UID 升序，便于稳定复现）。
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UIDValidity != result[j].UIDValidity {
			return result[i].UIDValidity < result[j].UIDValidity
		}
		return result[i].UID < result[j].UID
	})
	return result
}

// Get 返回指定记录。
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	return entry, ok
}

// Put 写入记录。同一 UID 已投递成功（sent）时直接跳过，保证重复处理幂等。
func (s *Store) Put(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.entries[entry.ID]; ok && existing.State == StateSent {
		return nil
	}
	return s.writeLocked(entry)
}

// Update 覆盖已有记录；记录不存在时返回错误，避免静默写入幽灵条目。
func (s *Store) Update(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[entry.ID]; !ok {
		return fmt.Errorf("待投递记录不存在: %s", entry.ID)
	}
	return s.writeLocked(entry)
}

// Delete 删除记录；记录不存在视为成功。
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, id)
	if err := os.Remove(filepath.Join(s.dir, id+fileSuffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除待投递记录失败: %w", err)
	}
	return nil
}

// Count 返回指定状态的记录数；state 为空表示全部。
func (s *Store) Count(state string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state == "" {
		return len(s.entries)
	}
	count := 0
	for _, entry := range s.entries {
		if entry.State == state {
			count++
		}
	}
	return count
}

// LastSent 返回同一讨论串最近一次成功投递的时间。
func (s *Store) LastSent(thread string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest time.Time
	for _, entry := range s.entries {
		if entry.State != StateSent || entry.Thread != thread {
			continue
		}
		if entry.UpdatedAt.After(latest) {
			latest = entry.UpdatedAt
		}
	}
	return latest
}

// RecoverExpiredLeases 把租约过期的 sending 记录恢复为 pending，返回恢复条数。
// 租约为零值视为已过期，用于兼容旧记录与进程异常退出。
func (s *Store) RecoverExpiredLeases(now time.Time, timeout time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	recovered := 0
	for _, entry := range s.entries {
		if entry.State != StateSending {
			continue
		}
		if timeout > 0 && entry.LeaseUntil.After(now) {
			continue
		}
		entry.State = StatePending
		entry.LeaseUntil = time.Time{}
		entry.ReadyAt = now
		entry.UpdatedAt = now
		if err := s.writeLocked(entry); err == nil {
			recovered++
		}
	}
	return recovered
}

// GC 删除超过保留期的终态记录（sent / failed / dropped）。
// 冷却判断依赖 sent 记录，keep 应大于 thread_cooldown。
func (s *Store) GC(now time.Time, keep time.Duration) int {
	if keep <= 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, entry := range s.entries {
		switch entry.State {
		case StateSent, StateFailed, StateDropped:
		default:
			continue
		}
		if now.Sub(entry.UpdatedAt) < keep {
			continue
		}
		delete(s.entries, id)
		if err := os.Remove(filepath.Join(s.dir, id+fileSuffix)); err == nil || errors.Is(err, os.ErrNotExist) {
			removed++
		}
	}
	return removed
}

func (s *Store) writeLocked(entry Entry) error {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化待投递记录失败: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(s.dir, entry.ID+fileSuffix)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("写入待投递临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换待投递记录失败: %w", err)
	}
	s.entries[entry.ID] = entry
	return nil
}

// Dir 返回存储目录。
func (s *Store) Dir() string { return s.dir }
