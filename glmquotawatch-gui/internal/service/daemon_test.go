package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"glmquotawatch-gui/internal/api"
	"glmquotawatch-gui/internal/quota"
	"glmquotawatch-gui/internal/store"
)

// fakeNotifier 捕获 daemon 发出的通知（真实通知依赖 Wails 服务，不进单测）。
type fakeNotifier struct {
	got      chan string
	failures int
	calls    int
}

func (f *fakeNotifier) Notify(title, msg string, silent bool) error {
	f.calls++
	if f.calls <= f.failures {
		return errors.New("notification ID cannot be empty")
	}
	f.got <- title + "|" + msg
	return nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestRunDaemonNotifies 验证编排链路：daemon 首轮采样跨档 → 调 Notifier
// 发送合并文案（报最高新档）→ ctx 取消后优雅退出。
func TestRunDaemonNotifies(t *testing.T) {
	pct := int64(60) // 触发 [50,60] 两档，应只报最高 60
	svc := newTestSvc(t, &pct)
	if _, err := svc.SetToken(validToken); err != nil {
		t.Fatal(err)
	}

	fn := &fakeNotifier{got: make(chan string, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- svc.RunDaemon(ctx, fn, quietLogger(), DaemonHooks{}) }()

	select {
	case m := <-fn.got:
		if !strings.HasPrefix(m, quota.AlertTitle+"|") {
			t.Fatalf("标题不符: %q", m)
		}
		if !strings.Contains(m, "阈值 60%") || strings.Contains(m, "阈值 50%") {
			t.Fatalf("应只报最高新档 60: %q", m)
		}
	case err := <-done:
		t.Fatalf("daemon 提前退出: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatalf("10s 内未收到通知")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancel 后应优雅退出: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("cancel 后 5s 未退出")
	}
}

// TestRunDaemonNoRepeat 验证同档位不重复通知：首轮触发后，后续采样轮静默。
func TestRunDaemonNoRepeat(t *testing.T) {
	pct := int64(60)
	svc := newTestSvc(t, &pct)
	if _, err := svc.SetToken(validToken); err != nil {
		t.Fatal(err)
	}

	fn := &fakeNotifier{got: make(chan string, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- svc.RunDaemon(ctx, fn, quietLogger(), DaemonHooks{}) }()

	select {
	case <-fn.got: // 首轮通知到达
	case <-time.After(10 * time.Second):
		t.Fatalf("10s 内未收到首轮通知")
	}

	// interval 最小 30s，等满一整轮采样周期，确认没有第二条
	select {
	case m := <-fn.got:
		t.Fatalf("同档位不应重复通知: %q", m)
	case err := <-done:
		t.Fatalf("daemon 提前退出: %v", err)
	case <-time.After(1 * time.Second):
	}
	cancel()
	<-done
}

// TestRunDaemonOnSampleHook 验证 onSample 回调每轮采样成功后收到最新视图。
func TestRunDaemonOnSampleHook(t *testing.T) {
	pct := int64(2) // 低用量，不触发通知
	svc := newTestSvc(t, &pct)
	if _, err := svc.SetToken(validToken); err != nil {
		t.Fatal(err)
	}

	got := make(chan StatusView, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- svc.RunDaemon(ctx, &fakeNotifier{got: make(chan string, 1)}, quietLogger(), DaemonHooks{OnSample: func(v StatusView) {
			select {
			case got <- v:
			default:
			}
		}})
	}()

	select {
	case v := <-got:
		if v.Level != "max" || len(v.Windows) != 1 || v.Windows[0].Percentage != 2 {
			t.Fatalf("回调视图不符: %+v", v)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("10s 内未收到 onSample 回调")
	}
	cancel()
	<-done
}

// TestRunDaemonFixedInterval 验证 fixedInterval 模式（演示）：
// 间隔直用固定值（不受 config interval 与 clamp 影响）、周期轮照常采样。
func TestRunDaemonFixedInterval(t *testing.T) {
	pct := int64(2)
	reset := time.Now().Add(4 * time.Hour).UnixMilli()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"code":200,"msg":"操作成功","success":true,"data":{"level":"max","limits":[`+
			`{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":%d,"nextResetTime":%d}]}}`, pct, reset)
	}))
	defer srv.Close()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// config 里写一个偏大的 interval，验证固定间隔模式完全忽略它
	if err := st.SaveConfig(store.Config{Token: validToken, Interval: "1h", Thresholds: []int{50, 60, 80, 90}, Hysteresis: 5}); err != nil {
		t.Fatal(err)
	}
	svc := New(st,
		WithFetcherFactory(func() api.Fetcher { return api.NewClient(srv.URL, validToken) }),
		WithFixedInterval(2*time.Second))

	got := make(chan StatusView, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- svc.RunDaemon(ctx, &fakeNotifier{got: make(chan string, 1)}, quietLogger(), DaemonHooks{OnSample: func(v StatusView) { got <- v }})
	}()

	// 固定 2s 间隔下，6s 内应收到 ≥3 轮回调（首轮 + 至少两个周期轮）
	deadline := time.After(6 * time.Second)
	count := 0
	for count < 3 {
		select {
		case v := <-got:
			if v.Level != "max" {
				t.Fatalf("回调视图不符: %+v", v)
			}
			count++
		case <-deadline:
			t.Fatalf("固定间隔下回调轮数不足: %d", count)
		}
	}
	cancel()
	<-done
}

func TestRunDaemonRetriesFailedNotificationBeforeState(t *testing.T) {
	pct := int64(60)
	reset := time.Now().Add(4 * time.Hour).UnixMilli()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"code":200,"msg":"操作成功","success":true,"data":{"level":"max","limits":[`+
			`{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":%d,"nextResetTime":%d}]}}`, pct, reset)
	}))
	defer srv.Close()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveConfig(store.Config{
		Token:      validToken,
		Thresholds: []int{50, 60, 80, 90},
		Hysteresis: 5,
	}); err != nil {
		t.Fatal(err)
	}
	svc := New(st,
		WithFetcherFactory(func() api.Fetcher { return api.NewClient(srv.URL, validToken) }),
		WithFixedInterval(10*time.Millisecond))

	fn := &fakeNotifier{got: make(chan string, 2), failures: 1}
	notifyErrs := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- svc.RunDaemon(ctx, fn, quietLogger(), DaemonHooks{
			OnNotifyError: func(err error) {
				select {
				case notifyErrs <- err.Error():
				default:
				}
			},
		})
	}()

	select {
	case msg := <-notifyErrs:
		if !strings.Contains(msg, "notify_failed") {
			t.Fatalf("应报告 notify_failed: %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("2s 内未收到通知失败回调")
	}

	state, err := st.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 0 || len(state.Notified) != 0 {
		t.Fatalf("通知失败后不应记账: %+v", state)
	}

	select {
	case msg := <-fn.got:
		if !strings.Contains(msg, "阈值 60%") {
			t.Fatalf("重试应保留未确认档位: %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("2s 内未完成通知重试")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		state, err = st.LoadState()
		if err != nil {
			t.Fatal(err)
		}
		if got := state.Notified["TOKENS_LIMIT:3:5"]; len(got) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("重试成功后应记账 [50,60]: %+v", state)
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
