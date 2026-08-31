package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry 是一条受管理的 EXE 记录；Valid 为运行时状态，不落盘。
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	AddedAt string `json:"added_at"`
	Valid   bool   `json:"-"`
}

type store struct {
	entries []Entry
}

// samePath Windows 路径大小写不敏感，先 Clean 再比较。
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// newStore 载入时清洗路径并去重。注意 Clean("") 会得到 "."，空路径要先判。
func newStore(entries []Entry) *store {
	s := &store{}
	for _, e := range entries {
		if e.Path == "" {
			continue
		}
		e.Path = filepath.Clean(e.Path)
		if s.containsPath(e.Path) {
			continue
		}
		s.entries = append(s.entries, e)
	}
	return s
}

func (s *store) containsPath(path string) bool {
	for _, e := range s.entries {
		if samePath(e.Path, path) {
			return true
		}
	}
	return false
}

// Add 添加记录；路径重复（大小写不敏感）时不添加并返回 false。
// name 为空时取文件名（去 .exe 后缀）。
func (s *store) Add(name, path string) bool {
	if path == "" {
		return false
	}
	path = filepath.Clean(path)
	if s.containsPath(path) {
		return false
	}
	if name == "" {
		name = exeBaseName(path)
	}
	s.entries = append(s.entries, Entry{
		Name:    name,
		Path:    path,
		AddedAt: time.Now().Format(time.RFC3339),
		Valid:   fileExists(path),
	})
	return true
}

func (s *store) Remove(index int) {
	if index < 0 || index >= len(s.entries) {
		return
	}
	s.entries = append(s.entries[:index], s.entries[index+1:]...)
}

// RemoveInvalid 剔除所有失效条目，返回剔除数量。
func (s *store) RemoveInvalid() int {
	kept := s.entries[:0]
	n := 0
	for _, e := range s.entries {
		if e.Valid {
			kept = append(kept, e)
		} else {
			n++
		}
	}
	s.entries = kept
	return n
}

func (s *store) RefreshValid() {
	for i := range s.entries {
		s.entries[i].Valid = fileExists(s.entries[i].Path)
	}
}

func (s *store) InvalidCount() int {
	n := 0
	for _, e := range s.entries {
		if !e.Valid {
			n++
		}
	}
	return n
}

func (s *store) Snapshot() []Entry {
	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
