package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func waitItem(t *testing.T, events <-chan Item) Item {
	t.Helper()
	select {
	case item := <-events:
		return item
	case <-time.After(2 * time.Second):
		t.Fatal("等待防抖回调超时")
		return Item{}
	}
}

func expectNoItem(t *testing.T, events <-chan Item, wait time.Duration) {
	t.Helper()
	select {
	case item := <-events:
		t.Fatalf("不应触发回调，却收到 %+v", item)
	case <-time.After(wait):
	}
}

func TestMergesEventsWithinWindow(t *testing.T) {
	events := make(chan Item, 10)
	d := New(30*time.Millisecond, 0, func(item Item) { events <- item })
	defer d.Close()

	if !d.Push(Item{Key: "repo#1", Data: 1}) ||
		!d.Push(Item{Key: "repo#1", Data: 2}) ||
		!d.Push(Item{Key: "repo#1", Data: 3}) {
		t.Fatal("窗口内的事件都应被接受")
	}

	item := waitItem(t, events)
	if item.Data != 3 {
		t.Errorf("应保留最新事件, got %+v", item)
	}
	expectNoItem(t, events, 60*time.Millisecond)
}

func TestDifferentKeysAreIndependent(t *testing.T) {
	events := make(chan Item, 10)
	d := New(20*time.Millisecond, 0, func(item Item) { events <- item })
	defer d.Close()

	d.Push(Item{Key: "repo#1", Data: 1})
	d.Push(Item{Key: "repo#2", Data: 2})

	got := map[string]any{}
	for i := 0; i < 2; i++ {
		item := waitItem(t, events)
		got[item.Key] = item.Data
	}
	if got["repo#1"] != 1 || got["repo#2"] != 2 {
		t.Errorf("不同 key 应各自触发, got %+v", got)
	}
}

func TestCooldownDropsEvents(t *testing.T) {
	events := make(chan Item, 10)
	d := New(10*time.Millisecond, 80*time.Millisecond, func(item Item) { events <- item })
	defer d.Close()

	d.Push(Item{Key: "k", Data: "first"})
	if item := waitItem(t, events); item.Data != "first" {
		t.Fatalf("首次回调 = %+v", item)
	}

	if d.Push(Item{Key: "k", Data: "during-cooldown"}) {
		t.Error("冷却期内的事件应被丢弃")
	}
	expectNoItem(t, events, 50*time.Millisecond)

	time.Sleep(60 * time.Millisecond)
	if !d.Push(Item{Key: "k", Data: "after-cooldown"}) {
		t.Fatal("冷却结束后应重新接受事件")
	}
	if item := waitItem(t, events); item.Data != "after-cooldown" {
		t.Errorf("冷却结束后的回调 = %+v", item)
	}
}

func TestCloseCancelsPending(t *testing.T) {
	events := make(chan Item, 10)
	d := New(100*time.Millisecond, 0, func(item Item) { events <- item })

	d.Push(Item{Key: "k", Data: 1})
	d.Close()
	expectNoItem(t, events, 150*time.Millisecond)

	if d.Push(Item{Key: "k", Data: 2}) {
		t.Error("Close 后不应再接受事件")
	}
	d.Close() // 幂等
}

func TestPushIsConcurrencySafe(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]int)
	d := New(5*time.Millisecond, 0, func(item Item) {
		mu.Lock()
		seen[item.Key]++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				d.Push(Item{Key: "k", Data: id})
			}
		}(i)
	}
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		count := seen["k"]
		mu.Unlock()
		if count > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("并发 Push 后应至少触发一次")
		}
		time.Sleep(5 * time.Millisecond)
	}
	d.Close()
}
