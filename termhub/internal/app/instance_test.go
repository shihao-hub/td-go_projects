// instance_test.go：单实例判存轮询的时序断言（互斥体与 HTTP 由 fake 注入）。
package app

import (
	"testing"
	"time"
)

func TestPollInstanceAcquire_PrimaryWhenMutexFree(t *testing.T) {
	calls := 0
	primary, url, busy := pollInstanceAcquire(50*time.Millisecond, time.Millisecond, func() (bool, string, bool) {
		calls++
		return true, "", false
	})
	if !primary || url != "" || busy {
		t.Fatalf("primary=%v url=%q busy=%v", primary, url, busy)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestPollInstanceAcquire_HandsOffToLiveInstance(t *testing.T) {
	calls := 0
	primary, url, busy := pollInstanceAcquire(50*time.Millisecond, time.Millisecond, func() (bool, string, bool) {
		calls++
		if calls < 3 {
			return false, "http://127.0.0.1:1", false
		}
		return false, "http://127.0.0.1:2", true
	})
	if primary || busy {
		t.Fatalf("primary=%v busy=%v", primary, busy)
	}
	if url != "http://127.0.0.1:2" {
		t.Fatalf("url = %q", url)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

// 旧进程退出期间：互斥体仍被占、实例不可达；它释放后本次轮询应接手成为首实例。
func TestPollInstanceAcquire_TakesOverAfterExitTeardown(t *testing.T) {
	calls := 0
	primary, url, busy := pollInstanceAcquire(200*time.Millisecond, time.Millisecond, func() (bool, string, bool) {
		calls++
		if calls < 4 {
			return false, "http://127.0.0.1:7927", false
		}
		return true, "", false
	})
	if !primary || busy || url != "" {
		t.Fatalf("primary=%v url=%q busy=%v", primary, url, busy)
	}
}

func TestPollInstanceAcquire_BusyAfterGrace(t *testing.T) {
	start := time.Now()
	primary, url, busy := pollInstanceAcquire(20*time.Millisecond, time.Millisecond, func() (bool, string, bool) {
		return false, "http://127.0.0.1:1", false
	})
	if primary || !busy {
		t.Fatalf("primary=%v busy=%v", primary, busy)
	}
	if url != "http://127.0.0.1:1" {
		t.Fatalf("url = %q", url)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("宽限期未生效，耗时 %v", elapsed)
	}
}
