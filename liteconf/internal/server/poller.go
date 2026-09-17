package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Poller 周期检测绕过 API 的外部文件编辑。
// Reload 内部先比 mtime，变化才算内容 hash，避免全量 hash 开销。
type Poller struct {
	st       *Store
	interval time.Duration
}

// NewPoller 创建检测器；interval 非正时回退默认 5s
func NewPoller(st *Store, interval time.Duration) *Poller {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Poller{st: st, interval: interval}
}

// Run 周期扫描配置根目录直到 ctx 取消：
// 新出现的合法 {app}/{env}.json 调用 Reload 注册（version 1）；
// 已存在 key 由 Reload 按 mtime/hash 判断变化。
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.scan()
		}
	}
}

// scan 单轮扫描：遍历两级目录，对全部合法 key 触发 Reload
func (p *Poller) scan() {
	entries, err := os.ReadDir(p.st.Root())
	if err != nil {
		slog.Warn("poller: read root failed", "root", p.st.Root(), "err", err)
		return
	}
	for _, appDir := range entries {
		if !appDir.IsDir() || !validName(appDir.Name()) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(p.st.Root(), appDir.Name()))
		if err != nil {
			slog.Warn("poller: read app dir failed", "app", appDir.Name(), "err", err)
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			env := strings.TrimSuffix(f.Name(), ".json")
			if !validName(env) {
				continue
			}
			p.reload(key{App: appDir.Name(), Env: env})
		}
	}
}

// reload 处理单个 key 的外部变化，按容错哲学告警但不动内存旧版本
func (p *Poller) reload(k key) {
	changed, err := p.st.Reload(k)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			slog.Warn("poller: config file removed, keep serving last version", "app", k.App, "env", k.Env)
		case errors.Is(err, ErrInvalidJSON):
			slog.Warn("poller: config file has invalid json, keep serving last version", "app", k.App, "env", k.Env, "err", err)
		default:
			slog.Warn("poller: reload failed", "app", k.App, "env", k.Env, "err", err)
		}
		return
	}
	if changed {
		slog.Info("poller: external edit detected", "app", k.App, "env", k.Env)
	}
}
