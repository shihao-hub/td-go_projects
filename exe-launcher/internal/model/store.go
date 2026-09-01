package model

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry 是一条受管理的 EXE 记录；Valid 为运行时状态，不落盘。
// SysTag 至多一个系统标签（tags.go 的固定枚举，空 = 无）；
// UserTag 至多一个用户标签（自由文本，描述语义）。
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	AddedAt string `json:"added_at"`
	SysTag  string `json:"sys_tag,omitempty"`
	UserTag string `json:"user_tag,omitempty"`
	Valid   bool   `json:"-"`
}

type Store struct {
	Entries []Entry
}

// samePath Windows 路径大小写不敏感，先 Clean 再比较。
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// NewStore 载入时清洗路径并去重。注意 Clean("") 会得到 "."，空路径要先判。
func NewStore(entries []Entry) *Store {
	s := &Store{}
	for _, e := range entries {
		if e.Path == "" {
			continue
		}
		e.Path = filepath.Clean(e.Path)
		if s.containsPath(e.Path) {
			continue
		}
		e.SysTag = SanitizeSysTag(e.SysTag)
		e.UserTag = strings.TrimSpace(e.UserTag)
		s.Entries = append(s.Entries, e)
	}
	return s
}

func (s *Store) containsPath(path string) bool {
	for _, e := range s.Entries {
		if samePath(e.Path, path) {
			return true
		}
	}
	return false
}

// Add 添加记录；路径重复（大小写不敏感）时不添加并返回 false。
// name 为空时取文件名（去 .exe 后缀）。
func (s *Store) Add(name, path string) bool {
	if path == "" {
		return false
	}
	path = filepath.Clean(path)
	if s.containsPath(path) {
		return false
	}
	if name == "" {
		name = ExeBaseName(path)
	}
	s.Entries = append(s.Entries, Entry{
		Name:    name,
		Path:    path,
		AddedAt: time.Now().Format(time.RFC3339),
		Valid:   FileExists(path),
	})
	return true
}

func (s *Store) Remove(index int) {
	if index < 0 || index >= len(s.Entries) {
		return
	}
	s.Entries = append(s.Entries[:index], s.Entries[index+1:]...)
}

// RemoveInvalid 剔除所有失效条目，返回剔除数量。
func (s *Store) RemoveInvalid() int {
	kept := s.Entries[:0]
	n := 0
	for _, e := range s.Entries {
		if e.Valid {
			kept = append(kept, e)
		} else {
			n++
		}
	}
	s.Entries = kept
	return n
}

func (s *Store) RefreshValid() {
	for i := range s.Entries {
		s.Entries[i].Valid = FileExists(s.Entries[i].Path)
	}
}

func (s *Store) InvalidCount() int {
	n := 0
	for _, e := range s.Entries {
		if !e.Valid {
			n++
		}
	}
	return n
}

func (s *Store) Snapshot() []Entry {
	out := make([]Entry, len(s.Entries))
	copy(out, s.Entries)
	return out
}

func FileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
