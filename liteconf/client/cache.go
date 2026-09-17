package client

import (
	"strings"
	"sync/atomic"
)

// cacheState 缓存快照：配置内容 + 本地已知版本
type cacheState struct {
	content map[string]any
	version uint64
}

// cache copy-on-write 快照缓存：
// 更新时整体替换 atomic.Pointer，读路径完全无锁
type cache struct {
	ptr atomic.Pointer[cacheState]
}

// swap 原子替换缓存快照
func (c *cache) swap(content map[string]any, version uint64) {
	c.ptr.Store(&cacheState{content: content, version: version})
}

// load 返回当前快照（可能为 nil，表示尚未初始化）
func (c *cache) load() *cacheState {
	return c.ptr.Load()
}

// version 返回本地已知版本，未初始化时为 0
func (c *cache) version() uint64 {
	if st := c.load(); st != nil {
		return st.version
	}
	return 0
}

// lookup 按 "db.host" 点路径逐级下钻 map[string]any；
// 任一层非 map 或键不存在返回 false（存在性标识，非静默零值）
func lookup(content map[string]any, path string) (any, bool) {
	if content == nil {
		return nil, false
	}
	if path == "" {
		return content, true
	}
	cur := any(content)
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// snapshot 返回缓存内容的浅拷贝，防调用方污染内部状态
func snapshotOf(content map[string]any) map[string]any {
	out := make(map[string]any, len(content))
	for k, v := range content {
		out[k] = v
	}
	return out
}
