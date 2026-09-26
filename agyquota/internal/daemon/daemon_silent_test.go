//go:build windows

package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agyquota/internal/agapi"
	"agyquota/internal/appdata"
	"agyquota/internal/service"
)

func getTestPEBinaryForDaemon(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("agy.exe"); err == nil {
		return p
	}
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		candidate := filepath.Join(localAppData, "agy", "bin", "agy.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	self, err := os.Executable()
	if err == nil {
		return self
	}
	t.Skip("未找到 PE 可执行文件用于测试")
	return ""
}

// TestDaemonRestartRetainsSilentCopy 验证 daemon 在重启全生命周期中持续使用已校验的静默副本
func TestDaemonRestartRetainsSilentCopy(t *testing.T) {
	srcPE := getTestPEBinaryForDaemon(t)

	// 使用隔离的 APPDATA 目录
	tempAppData := filepath.Join(t.TempDir(), "daemon_test_appdata")
	t.Setenv("APPDATA", tempAppData)

	binDir := filepath.Join(appdata.Dir(), "bin")
	silentExe := filepath.Join(binDir, "agy_silent.exe")
	silentSha := silentExe + ".sha256"

	// 1. 初始化 client 并调用 FetchUsage
	client1 := agapi.NewClient()
	client1.AgyPath = srcPE

	var loggedRuns []string
	client1.Logf = func(format string, args ...any) {
		loggedRuns = append(loggedRuns, fmt.Sprintf(format, args...))
	}

	// 验证第一次启动并生成静默副本
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 启动一次 daemon 模拟服务
	dataDir := appdata.Dir()
	cfg := Config{
		Bind:        "127.0.0.1",
		Port:        0, // 随机端口
		IdleTimeout: 5 * time.Minute,
		DataDir:     dataDir,
	}

	// 启动第一个 daemon 实例
	daemonCtx1, daemonCancel1 := context.WithCancel(context.Background())
	svc1 := service.New()
	errCh1 := make(chan error, 1)
	go func() {
		errCh1 <- serve(daemonCtx1, cfg, svc1)
	}()

	addrPath := filepath.Join(dataDir, appdata.AddressFileName)
	// 等待 address 文件生成
	var addr string
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		if b, err := os.ReadFile(addrPath); err == nil && len(b) > 0 {
			addr = string(b)
			break
		}
	}
	if addr == "" {
		t.Fatal("daemon 1 未能在预期时间内写入地址文件")
	}

	// 触发一次基于真实静默副本的准备与校验
	_, _ = client1.FetchUsage(ctx)

	// 校验静默副本与哈希文件确实已生成在 daemon 数据目录
	fi1, err := os.Stat(silentExe)
	if err != nil {
		t.Fatalf("静默副本未生成: %v", err)
	}
	if fi1.Size() == 0 {
		t.Fatal("静默副本大小为 0")
	}
	if _, err := os.Stat(silentSha); err != nil {
		t.Fatalf("静默副本哈希绑定文件未生成: %v", err)
	}

	// 停止第一个 daemon 实例
	daemonCancel1()
	select {
	case <-errCh1:
	case <-time.After(3 * time.Second):
		t.Fatal("daemon 1 退出超时")
	}

	// 2. 模拟重启：启动第二个 daemon 实例
	loggedRuns = nil
	daemonCtx2, daemonCancel2 := context.WithCancel(context.Background())
	defer daemonCancel2()

	svc2 := service.New()
	errCh2 := make(chan error, 1)
	go func() {
		errCh2 <- serve(daemonCtx2, cfg, svc2)
	}()

	// 等待第二个 daemon 就绪
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		if b, err := os.ReadFile(addrPath); err == nil && len(b) > 0 {
			break
		}
	}

	// 验证重启后，Client 执行查询时继续复用已有的静默副本
	client2 := agapi.NewClient()
	client2.AgyPath = srcPE
	client2.Logf = func(format string, args ...any) {
		loggedRuns = append(loggedRuns, fmt.Sprintf(format, args...))
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	_, _ = client2.FetchUsage(ctx2)

	// 校验日志中确实明确显示使用了静默副本
	var foundSilentLog bool
	for _, logLine := range loggedRuns {
		if strings.Contains(logLine, "agy_silent.exe") && strings.Contains(logLine, "使用静默副本: true") {
			foundSilentLog = true
			break
		}
	}
	if !foundSilentLog {
		t.Fatalf("重启后日志未显示使用静默副本: %v", loggedRuns)
	}

	// 校验静默副本时间戳未被意外修改（即复用缓存而非每次重写）
	fi2, err := os.Stat(silentExe)
	if err != nil {
		t.Fatalf("重启后静默副本丢失: %v", err)
	}
	if !fi2.ModTime().Equal(fi1.ModTime()) {
		t.Fatalf("静默副本未命中缓存，修改时间被重置: %v vs %v", fi2.ModTime(), fi1.ModTime())
	}

	daemonCancel2()
	_ = http.DefaultClient
}
