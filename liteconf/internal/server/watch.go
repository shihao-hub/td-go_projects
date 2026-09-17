package server

import (
	"net/http"
	"sync"
	"time"
)

// bc 单个配置 key 的广播器：
// close-broadcast 惯用法——广播时 close 旧 channel 并换新，唤醒所有等待者
type bc struct {
	mu sync.Mutex
	ch chan struct{}
}

// Broadcaster 每 key 一个广播器，支持任意多客户端并发订阅
type Broadcaster struct {
	mu   sync.Mutex
	keys map[key]*bc
}

// NewBroadcaster 创建广播器
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{keys: make(map[key]*bc)}
}

// Notify 唤醒订阅该 key 的所有等待者（store.onChanged 注入此方法）
func (b *Broadcaster) Notify(k key) {
	b.mu.Lock()
	cur, ok := b.keys[k]
	if !ok {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	cur.mu.Lock()
	close(cur.ch)
	cur.ch = make(chan struct{})
	cur.mu.Unlock()
}

// channel 懒创建该 key 的广播 channel
func (b *Broadcaster) channel(k key) chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	cur, ok := b.keys[k]
	if !ok {
		cur = &bc{ch: make(chan struct{})}
		b.keys[k] = cur
	}
	return cur.ch
}

// Wait 挂起等待 key 的版本变化：
// 三路 select——版本变化、超时、客户端断开。
// 返回 true 表示该 key 发生了变化（ch 被 close）。
func (b *Broadcaster) Wait(r *http.Request, k key, timeout time.Duration) bool {
	ch := b.channel(k)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ch:
		return true
	case <-timer.C:
		return false
	case <-r.Context().Done():
		return false
	}
}
