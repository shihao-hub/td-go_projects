package webui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"agyquota/internal/api"
	"agyquota/internal/appdata"
)

// lastQuotaFileName 是上次配额快照的落盘文件名。
// 桌面壳默认不发起网络查询，启动时直接展示这份缓存，用户点击「查询」才刷新。
const lastQuotaFileName = "last_quota.json"

// quotaCache 按数据源保存最近一次成功查询的快照（进程内 + 磁盘双份）。
type quotaCache struct {
	mu      sync.Mutex
	entries map[string]api.Snapshot
	logf    Logf
}

type quotaCacheFile struct {
	Version int                     `json:"version"`
	Entries map[string]api.Snapshot `json:"entries"`
}

func newQuotaCache(logf Logf) *quotaCache {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	c := &quotaCache{entries: map[string]api.Snapshot{}, logf: logf}
	c.loadFromDisk()
	return c
}

func quotaCachePath() string {
	dir := appdata.Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, lastQuotaFileName)
}

func (c *quotaCache) get(source string) (api.Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap, ok := c.entries[source]
	return snap, ok
}

func (c *quotaCache) put(snap *api.Snapshot) {
	if snap == nil || snap.Source == "" {
		return
	}
	c.mu.Lock()
	c.entries[snap.Source] = *snap
	payload := quotaCacheFile{Version: 1, Entries: c.entries}
	c.mu.Unlock()
	c.writeToDisk(payload)
}

func (c *quotaCache) loadFromDisk() {
	path := quotaCachePath()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var file quotaCacheFile
	if err := json.Unmarshal(raw, &file); err != nil {
		c.logf("解析上次配额缓存失败，忽略: %v", err)
		return
	}
	for source, snap := range file.Entries {
		if source == "" {
			continue
		}
		c.entries[source] = snap
	}
}

func (c *quotaCache) writeToDisk(payload quotaCacheFile) {
	path := quotaCachePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		c.logf("创建配额缓存目录失败: %v", err)
		return
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		c.logf("编码配额缓存失败: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		c.logf("写入配额缓存失败: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		c.logf("替换配额缓存失败: %v", err)
	}
}
