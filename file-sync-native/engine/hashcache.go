package engine

import (
	"bufio"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultMaxCacheEntries = 200_000

// CacheEntry 记录某个绝对路径在 (mtime, size) 下的已知哈希。
type CacheEntry struct {
	ModTime time.Time
	Size    int64
	Hash    string
	LastUse time.Time
}

// HashCache 以绝对路径为键的哈希缓存，gob 持久化。
// 损坏的缓存文件会被整体弃用，宁可重算也不阻断同步。
type HashCache struct {
	mu      sync.RWMutex
	path    string
	max     int
	entries map[string]CacheEntry

	hits    int
	misses  int
	stores  int
}

func NewHashCache(path string) *HashCache {
	return &HashCache{
		path:    path,
		max:     defaultMaxCacheEntries,
		entries: make(map[string]CacheEntry),
	}
}

func LoadHashCache(path string) *HashCache {
	c := NewHashCache(path)
	f, err := os.Open(path)
	if err != nil {
		return c
	}
	defer f.Close()
	var entries map[string]CacheEntry
	if err := gob.NewDecoder(bufio.NewReader(f)).Decode(&entries); err != nil {
		return NewHashCache(path)
	}
	c.entries = entries
	return c
}

// Windows 文件系统不区分大小写，统一小写做键。
func cacheKey(abs string) string {
	return strings.ToLower(abs)
}

// Get 报告该文件在 mtime+size 均未变时的已知哈希。
func (c *HashCache) Get(abs string, modTime time.Time, size int64) (string, bool) {
	k := cacheKey(abs)
	c.mu.RLock()
	e, ok := c.entries[k]
	c.mu.RUnlock()
	if !ok || e.Size != size || !e.ModTime.Equal(modTime) {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return "", false
	}
	c.mu.Lock()
	e.LastUse = time.Now()
	c.entries[k] = e
	c.hits++
	c.mu.Unlock()
	return e.Hash, true
}

func (c *HashCache) Put(abs string, modTime time.Time, size int64, hash string) {
	k := cacheKey(abs)
	c.mu.Lock()
	c.entries[k] = CacheEntry{ModTime: modTime, Size: size, Hash: hash, LastUse: time.Now()}
	c.stores++
	c.mu.Unlock()
}

// HashOf 返回文件哈希，优先取缓存；bypass=true 用于强制校验（只写不读缓存）。
func (c *HashCache) HashOf(abs string, modTime time.Time, size int64, bypass bool) (string, error) {
	if !bypass {
		if h, ok := c.Get(abs, modTime, size); ok {
			return h, nil
		}
	}
	h, err := computeHash(abs)
	if err != nil {
		return "", err
	}
	c.Put(abs, modTime, size, h)
	return h, nil
}

func (c *HashCache) Stats() (hits, misses, stores, size int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits, c.misses, c.stores, len(c.entries)
}

// Save 持久化缓存；超限时按 LastUse 做 LRU 截断。
func (c *HashCache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) > c.max {
		c.evictLocked()
	}
	if err := os.MkdirAll(dirOf(c.path), 0o755); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	if err := gob.NewEncoder(w).Encode(c.entries); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *HashCache) evictLocked() {
	type kv struct {
		k  string
		lu time.Time
	}
	all := make([]kv, 0, len(c.entries))
	for k, e := range c.entries {
		all = append(all, kv{k: k, lu: e.LastUse})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].lu.After(all[j].lu) })
	next := make(map[string]CacheEntry, c.max)
	for _, e := range all[:c.max] {
		next[e.k] = c.entries[e.k]
	}
	c.entries = next
}

func computeHash(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1024*1024)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func dirOf(p string) string {
	i := strings.LastIndexByte(p, os.PathSeparator)
	if i <= 0 {
		return "."
	}
	return p[:i]
}
