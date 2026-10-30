package agy

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
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

func TestTitlerSkipIfTitledInZedDB(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "store.json")
	mockZedDB := filepath.Join(tempDir, "zed.sqlite")

	// 构造包含已有线程的模拟 Zed 数据库
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
			('sess_zed_hist', '分析acp标题代理项目', NULL);
	`)
	if err != nil {
		t.Fatalf("failed to setup mock db: %v", err)
	}
	db.Close()

	st, err := store.NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	buf := &bytes.Buffer{}
	inj := proxy.NewInjector(buf)
	titler := NewTitler(&config.AGYConfig{}, inj, st)
	titler.SetZedDBPath(mockZedDB)

	// User sends "继续"
	titler.OnPrompt("sess_zed_hist", "继续")

	time.Sleep(50 * time.Millisecond)

	// 1. 绝不允许向 Zed 注入任何标题
	if buf.Len() > 0 {
		t.Fatalf("expected no injection for session already existing in Zed DB, but got: %s", buf.String())
	}

	// 2. 必须回填存储，使后续检查命中存储快路径
	if !st.IsTitled("sess_zed_hist") {
		t.Fatalf("expected sess_zed_hist to be backfilled into store")
	}
	title, _ := st.GetTitle("sess_zed_hist")
	if title != "分析acp标题代理项目" {
		t.Fatalf("expected backfilled title '分析acp标题代理项目', got %q", title)
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
