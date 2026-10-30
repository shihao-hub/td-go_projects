package agy

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/proxy"
	"language_projects/go_projects/acp-title-proxy/internal/store"
)

func TestTitlerSkipIfTitledInStore(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "store.json")

	st, err := store.NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// 预先登记该会话已命名
	if err := st.MarkTitled("sess_existing", "已有的历史标题"); err != nil {
		t.Fatalf("failed to mark titled: %v", err)
	}

	buf := &bytes.Buffer{}
	inj := proxy.NewInjector(buf)
	titler := NewTitler(&config.AGYConfig{}, inj, st)

	// User sends continuation message "继续"
	titler.OnPrompt("sess_existing", "继续")

	// 短暂休眠，确认没有异步 goroutine 被拉起
	time.Sleep(50 * time.Millisecond)

	if buf.Len() > 0 {
		t.Fatalf("expected no injection for session already titled in store, but got: %s", buf.String())
	}
}

// newMockTitleServer 启动一个返回固定标题的假 OpenAI 兼容服务。
func newMockTitleServer(t *testing.T, title string, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, title)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func mockProvider(baseURL string) *config.AGYConfig {
	return &config.AGYConfig{
		Providers: []config.ProviderConfig{{
			Name:      "mock",
			Enabled:   true,
			BaseURL:   baseURL,
			APIKey:    "test-key",
			Model:     "mock-model",
			TimeoutMs: 2000,
		}},
	}
}

func waitUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// TestTitlerGeneratesDespitePlaceholderTitle 是本次修复的核心回归测试：
// Zed 在首条消息时就会把消息原文写进 sidebar_threads.title，
// 这类占位标题不得被当作"已有标题"而阻止生成。
func TestTitlerGeneratesDespitePlaceholderTitle(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "store.json")
	mockZedDB := filepath.Join(tempDir, "zed.sqlite")

	db, err := sql.Open("sqlite", mockZedDB)
	if err != nil {
		t.Fatalf("failed to open mock zed db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE sidebar_threads (
			session_id TEXT,
			title TEXT,
			title_override TEXT
		);
		INSERT INTO sidebar_threads (session_id, title, title_override) VALUES
			('sess_placeholder', '这部分是如何实现的？', NULL);
	`)
	if err != nil {
		t.Fatalf("failed to setup mock db: %v", err)
	}
	db.Close()

	st, err := store.NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	srv := newMockTitleServer(t, "池化逻辑解析", nil)

	buf := &bytes.Buffer{}
	inj := proxy.NewInjector(buf)
	titler := NewTitler(mockProvider(srv.URL), inj, st)
	titler.SetZedDBPath(mockZedDB)

	titler.OnPrompt("sess_placeholder", "这部分是如何实现的？")

	if !waitUntil(func() bool { return buf.Len() > 0 }, 2*time.Second) {
		t.Fatalf("expected title injection despite placeholder title, got none")
	}

	if out := buf.String(); !strings.Contains(out, "池化逻辑解析") {
		t.Fatalf("expected injected title in output, got: %s", out)
	}

	title, ok := st.GetTitle("sess_placeholder")
	if !ok || title != "池化逻辑解析" {
		t.Fatalf("expected store title '池化逻辑解析', got %q (ok=%v)", title, ok)
	}

	// 第二次消息不得重复生成
	before := buf.Len()
	titler.OnPrompt("sess_placeholder", "继续")
	time.Sleep(100 * time.Millisecond)
	if buf.Len() != before {
		t.Fatalf("expected no second injection, but output grew: %s", buf.String())
	}
}

// TestTitlerRespectsUserOverride 验证用户手动重命名后不再被自动标题覆盖。
func TestTitlerRespectsUserOverride(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "store.json")
	mockZedDB := filepath.Join(tempDir, "zed.sqlite")

	db, err := sql.Open("sqlite", mockZedDB)
	if err != nil {
		t.Fatalf("failed to open mock zed db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE sidebar_threads (
			session_id TEXT,
			title TEXT,
			title_override TEXT
		);
		INSERT INTO sidebar_threads (session_id, title, title_override) VALUES
			('sess_manual', '原始标题', '手动命名');
	`)
	if err != nil {
		t.Fatalf("failed to setup mock db: %v", err)
	}
	db.Close()

	st, err := store.NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	srv := newMockTitleServer(t, "自动标题", nil)

	buf := &bytes.Buffer{}
	inj := proxy.NewInjector(buf)
	titler := NewTitler(mockProvider(srv.URL), inj, st)
	titler.SetZedDBPath(mockZedDB)

	titler.OnPrompt("sess_manual", "随便问点什么")
	time.Sleep(150 * time.Millisecond)

	if buf.Len() > 0 {
		t.Fatalf("expected no injection for user-renamed session, got: %s", buf.String())
	}
	title, ok := st.GetTitle("sess_manual")
	if !ok || title != "手动命名" {
		t.Fatalf("expected store title '手动命名', got %q (ok=%v)", title, ok)
	}
}

// TestTitlerSkipsExistingGeneratedTitleAfterRestart 覆盖既有计划的验收场景：
// 进程重启（本地 store 未命中）后，在已有真实标题的会话里发“继续”，不得覆写标题。
func TestTitlerSkipsExistingGeneratedTitleAfterRestart(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "store.json")
	mockZedDB := filepath.Join(tempDir, "zed.sqlite")

	db, err := sql.Open("sqlite", mockZedDB)
	if err != nil {
		t.Fatalf("failed to open mock zed db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE sidebar_threads (
			session_id TEXT,
			title TEXT,
			title_override TEXT
		);
		INSERT INTO sidebar_threads (session_id, title, title_override) VALUES
			('sess_restart', '池化逻辑解析', NULL);
	`)
	if err != nil {
		t.Fatalf("failed to setup mock db: %v", err)
	}
	db.Close()

	// 全新 store，模拟代理进程重启后的冷启动
	st, err := store.NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	var hits int32
	srv := newMockTitleServer(t, "继续", &hits)

	buf := &bytes.Buffer{}
	titler := NewTitler(mockProvider(srv.URL), proxy.NewInjector(buf), st)
	titler.SetZedDBPath(mockZedDB)

	titler.OnPrompt("sess_restart", "继续")
	time.Sleep(150 * time.Millisecond)

	if buf.Len() > 0 {
		t.Fatalf("expected no injection for session with existing title, got: %s", buf.String())
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("expected no LLM call, got %d", got)
	}
	if title, ok := st.GetTitle("sess_restart"); !ok || title != "池化逻辑解析" {
		t.Fatalf("expected store title '池化逻辑解析', got %q (ok=%v)", title, ok)
	}
}

func TestIsPlaceholderTitle(t *testing.T) {
	cases := []struct {
		name   string
		title  string
		prompt string
		want   bool
	}{
		{"完全相同", "这部分是如何实现的？", "这部分是如何实现的?", true},
		{"带文件引用前缀", "这部分是如何实现的？", "main.go (76:93) 这部分是如何实现的?", true},
		{"标题被截断", "分析acp标题代理项目", "分析 acp 标题代理项目的实现与结构设计", true},
		{"真实标题与延续词", "池化逻辑解析", "继续", false},
		{"真实标题与追问", "池化逻辑解析", "那 worker 怎么复用", false},
	}
	for _, c := range cases {
		if got := isPlaceholderTitle(c.title, c.prompt); got != c.want {
			t.Errorf("%s: isPlaceholderTitle(%q, %q) = %v, want %v", c.name, c.title, c.prompt, got, c.want)
		}
	}
}

// TestTitlerStopsAfterMaxAttempts 验证模型持续失败时不会无限重试。
func TestTitlerStopsAfterMaxAttempts(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "store.json")

	st, err := store.NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	t.Cleanup(srv.Close)

	buf := &bytes.Buffer{}
	titler := NewTitler(mockProvider(srv.URL), proxy.NewInjector(buf), st)
	// 隔离真实 Zed 数据库：指向不存在的路径，确保走生成分支
	titler.SetZedDBPath(filepath.Join(tempDir, "zed-missing.sqlite"))

	for i := 0; i < 6; i++ {
		titler.OnPrompt("sess_fail", "触发失败的提问")
		time.Sleep(80 * time.Millisecond)
	}

	waitUntil(func() bool { return atomic.LoadInt32(&hits) >= maxTitleAttempts }, time.Second)

	if got := atomic.LoadInt32(&hits); got != maxTitleAttempts {
		t.Fatalf("expected %d attempts, got %d", maxTitleAttempts, got)
	}
	if buf.Len() > 0 {
		t.Fatalf("expected no injection on failure, got: %s", buf.String())
	}
}

func TestTitlerSurvivesProcessRestartSimulation(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "store.json")

	// 会话 1：进程 1 为该会话命名
	st1, _ := store.NewStore(storePath)
	_ = st1.MarkTitled("sess_reboot_test", "首条提问总结出来的标题")

	// 会话 2：进程 2 全新启动（模拟 Zed 或 agent 重启）
	st2, err := store.NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to reload store: %v", err)
	}

	buf2 := &bytes.Buffer{}
	inj2 := proxy.NewInjector(buf2)
	titler2 := NewTitler(&config.AGYConfig{}, inj2, st2)

	// In rebooted process, user says "继续"
	titler2.OnPrompt("sess_reboot_test", "继续")

	time.Sleep(50 * time.Millisecond)

	if buf2.Len() > 0 {
		t.Fatalf("process 2 should NOT overwrite title with '继续', but output: %s", buf2.String())
	}
}

func TestProviderChain(t *testing.T) {
	cfg, _, err := config.LoadConfig()
	if err != nil {
		t.Skip("config not loaded")
	}

	if len(cfg.AGY.Providers) == 0 {
		t.Skip("No AGY providers configured, skipping")
	}

	inj := proxy.NewInjector(os.Stdout)
	titler := NewTitler(&cfg.AGY, inj, nil)

	testPrompt := "/plan 在当前项目实现一个高性能的 Redis 缓存层，并增加布隆过滤器防止缓存穿透"
	title, err := titler.CallLLMDirect(testPrompt)
	if err != nil {
		t.Fatalf("CallLLMDirect failed: %v", err)
	}

	t.Logf("Primary provider generated title: %q", title)
	if title == "" {
		t.Errorf("Expected non-empty title")
	}
}

func TestFallbackToDeepSeek(t *testing.T) {
	cfg, _, err := config.LoadConfig()
	if err != nil {
		t.Skip("config not loaded")
	}

	// 禁用 opencode-go，验证回退到 deepseek-official
	mockCfg := *cfg
	mockCfg.AGY.Providers = make([]config.ProviderConfig, len(cfg.AGY.Providers))
	copy(mockCfg.AGY.Providers, cfg.AGY.Providers)

	for i := range mockCfg.AGY.Providers {
		if mockCfg.AGY.Providers[i].Name == "opencode-go" {
			mockCfg.AGY.Providers[i].Enabled = false
		}
	}

	inj := proxy.NewInjector(os.Stdout)
	titler := NewTitler(&mockCfg.AGY, inj, nil)

	testPrompt := "/plan 调研一下当前系统的网络带宽占用情况"
	title, err := titler.CallLLMDirect(testPrompt)
	if err != nil {
		t.Fatalf("Fallback failed: %v", err)
	}

	t.Logf("Fallback provider generated title: %q", title)
	if title == "" {
		t.Errorf("Expected non-empty title from fallback")
	}
}
