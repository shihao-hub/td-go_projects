package client

import "sync"

// callbacks OnChange 回调注册表与异步派发
type callbacks struct {
	mu  sync.Mutex
	fns []func(old, new map[string]any)
}

// OnChange 注册配置变更回调：版本变化时以（旧配置, 新配置）调用。
// 回调在独立 goroutine 中异步派发，单个回调 panic/阻塞不影响
// 其他回调与主轮询循环；不保证回调间执行顺序。
func (c *Client) OnChange(fn func(old, new map[string]any)) {
	if fn == nil {
		return
	}
	c.cb.mu.Lock()
	c.cb.fns = append(c.cb.fns, fn)
	c.cb.mu.Unlock()
}

// dispatch 以注册时快照逐个异步派发回调
func (c *Client) dispatch(old, new map[string]any) {
	c.cb.mu.Lock()
	fns := make([]func(old, new map[string]any), len(c.cb.fns))
	copy(fns, c.cb.fns)
	c.cb.mu.Unlock()

	for _, fn := range fns {
		go func(fn func(old, new map[string]any)) {
			defer func() { _ = recover() }()
			fn(old, new)
		}(fn)
	}
}
