package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheHitMiss(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	mustWrite(t, p, []byte("hello"))

	mt := statModTime(t, p)
	c := NewHashCache(filepath.Join(dir, "cache.gob"))

	if _, ok := c.Get(p, mt, 5); ok {
		t.Fatal("empty cache should miss")
	}
	h1 := hashOf(t, c, p, mt, 5)

	if h2, ok := c.Get(p, mt, 5); !ok || h2 != h1 {
		t.Fatalf("expected hit with same hash, got ok=%v h=%s", ok, h2)
	}

	mt2 := mt.Add(2 * time.Second)
	if _, ok := c.Get(p, mt2, 5); ok {
		t.Fatal("different mtime must miss")
	}
	if _, ok := c.Get(p, mt, 6); ok {
		t.Fatal("different size must miss")
	}

	_, ms, st, _ := c.Stats()
	if st != 1 || ms != 4 {
		t.Fatalf("stats: stores=%d misses=%d, want 1/4（HashOf 内部还有一次 miss）", st, ms)
	}
}

func TestCachePersist(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	mustWrite(t, p, []byte("world"))
	mt := statModTime(t, p)

	cachePath := filepath.Join(dir, "cache.gob")
	c1 := NewHashCache(cachePath)
	h1 := hashOf(t, c1, p, mt, 5)
	if err := c1.Save(); err != nil {
		t.Fatal(err)
	}

	c2 := LoadHashCache(cachePath)
	if h2, ok := c2.Get(p, mt, 5); !ok || h2 != h1 {
		t.Fatalf("persisted cache should hit, ok=%v", ok)
	}
}

func TestCacheCorruptFileRecovers(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.gob")
	if err := os.WriteFile(cachePath, []byte("not a gob stream"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := LoadHashCache(cachePath)
	if _, _, _, size := c.Stats(); size != 0 {
		t.Fatalf("corrupt cache should load empty, got %d entries", size)
	}
}

func TestCacheLRUEviction(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.gob")
	c := NewHashCache(cachePath)
	c.max = 3
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		key := cacheKey(filepath.Join(dir, "f", string(rune('a'+i))))
		c.Put(key, base, int64(i), "h")
		// Put 会把 LastUse 设为当下，这里按写入顺序拉开差距
		e := c.entries[key]
		e.LastUse = base.Add(time.Duration(i) * time.Minute)
		c.entries[key] = e
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, size := c.Stats(); size != 3 {
		t.Fatalf("want 3 entries after eviction, got %d", size)
	}
	// 最老的 a/b 应被淘汰，最新的 c/d/e 保留
	for _, keep := range []string{"c", "d", "e"} {
		if _, ok := c.entries[cacheKey(filepath.Join(dir, "f", keep))]; !ok {
			t.Errorf("entry %s should survive LRU", keep)
		}
	}
}

func mustWrite(t testing.TB, p string, data []byte) {
	if t != nil {
		t.Helper()
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func statModTime(t *testing.T, p string) time.Time {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime()
}

func hashOf(t *testing.T, c *HashCache, p string, mt time.Time, size int64) string {
	t.Helper()
	h, err := c.HashOf(p, mt, size, false)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
