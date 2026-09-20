package service

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"glmquotawatch/internal/quota"
)

// fakeNotifier 捕获 daemon 发出的通知（notify 包依赖真实 PowerShell，不进单测）。
type fakeNotifier struct {
	got chan string
}

func (f *fakeNotifier) Notify(title, msg string) error {
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
	go func() { done <- svc.RunDaemon(ctx, fn, quietLogger()) }()

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
	go func() { done <- svc.RunDaemon(ctx, fn, quietLogger()) }()

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

// TestRunDaemonLocked 验证锁被占时返回 locked 业务错误（含 PID 文案）。
func TestRunDaemonLocked(t *testing.T) {
	pct := int64(2)
	svc := newTestSvc(t, &pct)
	if _, err := svc.SetToken(validToken); err != nil {
		t.Fatal(err)
	}
	release, err := svc.st.Lock() // 外部占锁（模拟另一实例）
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	err = svc.RunDaemon(context.Background(), &fakeNotifier{got: make(chan string, 1)}, quietLogger())
	if svcErrCode(t, err) != "locked" {
		t.Fatalf("应 locked: %v", err)
	}
}
