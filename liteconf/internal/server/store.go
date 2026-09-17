package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrNotFound 表示配置不存在
var ErrNotFound = errors.New("config not found")

// ErrInvalidName 表示 app/env 名称不合法
var ErrInvalidName = errors.New("invalid app or env name")

// ErrInvalidJSON 表示配置内容不是合法的顶层 JSON 对象
var ErrInvalidJSON = errors.New("invalid json object")

// key 唯一标识一份配置
type key struct {
	App string
	Env string
}

// meta 配置内存元数据：版本单调递增、内容 hash、最后修改时间
type meta struct {
	Version uint64
	Hash    string
	ModTime time.Time
	Raw     json.RawMessage
}

// Entry 发现接口的单条条目
type Entry struct {
	App     string `json:"app"`
	Env     string `json:"env"`
	Version uint64 `json:"version"`
}

// Store 配置存储：文件即数据库，内存维护版本元数据。
// 广播通过注入 onChanged 回调解耦，store 不依赖 watch 包。
type Store struct {
	mu        sync.RWMutex
	root      string
	items     map[key]*meta
	onChanged func(key)
}

// NewStore 创建存储：确保配置根目录存在并加载已有配置
func NewStore(root string, onChanged func(key)) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create config root %q: %w", root, err)
	}
	s := &Store{root: root, items: make(map[key]*meta), onChanged: onChanged}
	s.LoadAll()
	return s, nil
}

// Root 返回配置根目录
func (s *Store) Root() string { return s.root }

// LoadAll 启动扫描两级目录 configs/{app}/{env}.json；
// 非法名称/非法 JSON 跳过并告警，合法项版本从 1 重建（重启后不承诺连续）
func (s *Store) LoadAll() {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		slog.Warn("load configs: read root failed", "root", s.root, "err", err)
		return
	}
	for _, appDir := range entries {
		if !appDir.IsDir() || !validName(appDir.Name()) {
			if !appDir.IsDir() || !strings.HasPrefix(appDir.Name(), ".") {
				slog.Warn("load configs: skip invalid app dir", "name", appDir.Name())
			}
			continue
		}
		files, err := os.ReadDir(filepath.Join(s.root, appDir.Name()))
		if err != nil {
			slog.Warn("load configs: read app dir failed", "app", appDir.Name(), "err", err)
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			env := strings.TrimSuffix(f.Name(), ".json")
			if !validName(env) {
				slog.Warn("load configs: skip invalid env file", "app", appDir.Name(), "file", f.Name())
				continue
			}
			k := key{App: appDir.Name(), Env: env}
			if _, err := s.readFile(k); err != nil {
				slog.Warn("load configs: skip invalid config", "app", k.App, "env", k.Env, "err", err)
				continue
			}
		}
	}
	slog.Info("load configs done", "count", len(s.snapshot()))
}

// Get 返回配置内容与版本的拷贝
func (s *Store) Get(k key) (json.RawMessage, uint64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.items[k]
	if !ok {
		return nil, 0, false
	}
	raw := make(json.RawMessage, len(m.Raw))
	copy(raw, m.Raw)
	return raw, m.Version, true
}

// List 返回全部 {app, env, version}，按 app、env 排序供发现接口
func (s *Store) List() []Entry {
	items := s.snapshot()
	out := make([]Entry, 0, len(items))
	for k, m := range items {
		out = append(out, Entry{App: k.App, Env: k.Env, Version: m.Version})
	}
	sortEntries(out)
	return out
}

// Put 校验并原子写入配置：临时文件 + rename 覆盖，版本 +1 后广播。
// 任一步失败文件与版本保持不变。
func (s *Store) Put(app, env string, body []byte) (uint64, error) {
	if !validName(app) || !validName(env) {
		return 0, ErrInvalidName
	}
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		return 0, fmt.Errorf("%w: body is not a json object: %w", ErrInvalidJSON, err)
	}

	dir := filepath.Join(s.root, app)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create app dir: %w", err)
	}
	target := filepath.Join(dir, env+".json")
	tmp, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		return 0, fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return 0, fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return 0, fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return 0, fmt.Errorf("rename temp file: %w", err)
	}
	tmpName = "" // rename 成功，无需清理

	k := key{App: app, Env: env}
	s.mu.Lock()
	old, ok := s.items[k]
	newVersion := uint64(1)
	if ok {
		newVersion = old.Version + 1
	}
	info, err := os.Stat(target)
	var modTime time.Time
	if err == nil {
		modTime = info.ModTime()
	}
	s.items[k] = &meta{Version: newVersion, Hash: hashBytes(body), ModTime: modTime, Raw: normalizeJSON(body)}
	s.mu.Unlock()

	s.notify(k)
	return newVersion, nil
}

// Reload 供外部编辑检测使用：文件内容与内存 hash 一致则不动；
// 未变化返回 false；非法 JSON 返回错误且保留旧版本；
// 合法变化则版本 +1、更新内存并广播。文件消失返回 os.ErrNotExist。
func (s *Store) Reload(k key) (bool, error) {
	s.mu.RLock()
	old, ok := s.items[k]
	s.mu.RUnlock()

	info, err := os.Stat(s.path(k))
	if err != nil {
		return false, fmt.Errorf("%w: %w", fs.ErrNotExist, err)
	}
	if ok && info.ModTime().Equal(old.ModTime) {
		return false, nil
	}
	m, err := s.readFile(k)
	if err != nil {
		return false, err
	}
	if ok && m.Hash == old.Hash {
		// 内容未变，仅刷新 mtime 基准
		s.mu.Lock()
		if cur, ok := s.items[k]; ok {
			cur.ModTime = info.ModTime()
		}
		s.mu.Unlock()
		return false, nil
	}

	s.mu.Lock()
	m.Version = 1
	if ok {
		m.Version = old.Version + 1
	}
	s.items[k] = m
	s.mu.Unlock()

	s.notify(k)
	return true, nil
}

// readFile 读取并校验单个配置文件，注册到内存（版本从 1 重建）
func (s *Store) readFile(k key) (*meta, error) {
	body, err := os.ReadFile(s.path(k))
	if err != nil {
		return nil, err
	}
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidJSON, err)
	}
	info, err := os.Stat(s.path(k))
	var modTime time.Time
	if err == nil {
		modTime = info.ModTime()
	}
	m := &meta{Version: 1, Hash: hashBytes(body), ModTime: modTime, Raw: normalizeJSON(body)}

	s.mu.Lock()
	if old, ok := s.items[k]; ok {
		m.Version = old.Version + 1
	}
	s.items[k] = m
	s.mu.Unlock()
	return m, nil
}

// path 返回配置文件路径 configs/{app}/{env}.json
func (s *Store) path(k key) string {
	return filepath.Join(s.root, k.App, k.Env+".json")
}

// notify 触发变更广播（回调可能为空）
func (s *Store) notify(k key) {
	if s.onChanged != nil {
		s.onChanged(k)
	}
}

// snapshot 锁内拷贝 items 快照
func (s *Store) snapshot() map[key]*meta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[key]*meta, len(s.items))
	for k, m := range s.items {
		cp := *m
		out[k] = &cp
	}
	return out
}

// normalizeJSON 规范化 JSON 字节（去除前后空白），空则视为 null
func normalizeJSON(body []byte) json.RawMessage {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return json.RawMessage("null")
	}
	return json.RawMessage(trimmed)
}

// hashBytes 计算内容 sha256 hex
func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// sortEntries 按 app、env 排序
func sortEntries(entries []Entry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			a, b := entries[j-1], entries[j]
			if a.App < b.App || (a.App == b.App && a.Env <= b.Env) {
				break
			}
			entries[j-1], entries[j] = entries[j], entries[j-1]
		}
	}
}
