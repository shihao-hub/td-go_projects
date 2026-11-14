// Package ratelimit 提供按讨论串聚合的防抖器。
//
// 语义：
//   - 同一 key 的首次事件进入防抖窗口，窗口结束前到达的事件只更新待推送
//     内容（保留最新），窗口结束后触发一次回调；
//   - 回调触发后进入冷却期，冷却期内同 key 的事件直接丢弃；
//   - 不同 key 互不影响。
package ratelimit

import (
	"sync"
	"time"
)

// Item 是一次待推送事件。Key 通常是"仓库#编号"或邮件 Message-ID。
type Item struct {
	Key  string
	Data any
}

type entry struct {
	item  Item
	timer *time.Timer
}

// Debouncer 按 key 做防抖与冷却。
type Debouncer struct {
	window   time.Duration
	cooldown time.Duration
	handler  func(Item)
	now      func() time.Time

	mu       sync.Mutex
	entries  map[string]*entry
	lastSent map[string]time.Time
	closed   bool
	inFlight sync.WaitGroup
}

// New 创建防抖器。window 与 cooldown 必须为正；cooldown 可为 0 表示不冷却。
func New(window, cooldown time.Duration, handler func(Item)) *Debouncer {
	if window <= 0 {
		window = time.Second
	}
	if cooldown < 0 {
		cooldown = 0
	}
	return &Debouncer{
		window:   window,
		cooldown: cooldown,
		handler:  handler,
		now:      time.Now,
		entries:  make(map[string]*entry),
		lastSent: make(map[string]time.Time),
	}
}

// Push 提交事件。返回 false 表示事件因冷却期或已关闭而被丢弃。
func (d *Debouncer) Push(item Item) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return false
	}

	now := d.now()
	if last, ok := d.lastSent[item.Key]; ok {
		if now.Sub(last) < d.cooldown {
			return false
		}
		delete(d.lastSent, item.Key)
	}

	if existing, ok := d.entries[item.Key]; ok {
		existing.item = item
		return true
	}
	pending := &entry{item: item}
	d.entries[item.Key] = pending
	key := item.Key
	pending.timer = time.AfterFunc(d.window, func() { d.flush(key) })
	return true
}

func (d *Debouncer) flush(key string) {
	d.mu.Lock()
	pending, ok := d.entries[key]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.entries, key)
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.lastSent[key] = d.now()
	d.pruneLocked()
	d.inFlight.Add(1)
	d.mu.Unlock()

	defer d.inFlight.Done()
	d.handler(pending.item)
}

// pruneLocked 清理已过冷却期的记录，避免 lastSent 无限增长。
func (d *Debouncer) pruneLocked() {
	now := d.now()
	for key, sentAt := range d.lastSent {
		if now.Sub(sentAt) >= d.cooldown {
			delete(d.lastSent, key)
		}
	}
}

// Close 停止接收新事件并取消所有待触发事件；已在执行的回调会等待完成。
func (d *Debouncer) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	for key, pending := range d.entries {
		pending.timer.Stop()
		delete(d.entries, key)
	}
	d.mu.Unlock()
	d.inFlight.Wait()
}
