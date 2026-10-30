package agy

import (
	"os"
	"testing"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/proxy"
)

func TestProviderChain(t *testing.T) {
	cfg, _, err := config.LoadConfig()
	if err != nil {
		t.Skip("config not loaded")
	}

	if len(cfg.AGY.Providers) == 0 {
		t.Skip("No AGY providers configured, skipping")
	}

	inj := proxy.NewInjector(os.Stdout)
	titler := NewTitler(&cfg.AGY, inj)

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

	// Disable opencode-go to test fallback to deepseek-official
	mockCfg := *cfg
	mockCfg.AGY.Providers = make([]config.ProviderConfig, len(cfg.AGY.Providers))
	copy(mockCfg.AGY.Providers, cfg.AGY.Providers)

	for i := range mockCfg.AGY.Providers {
		if mockCfg.AGY.Providers[i].Name == "opencode-go" {
			mockCfg.AGY.Providers[i].Enabled = false
		}
	}

	inj := proxy.NewInjector(os.Stdout)
	titler := NewTitler(&mockCfg.AGY, inj)

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
