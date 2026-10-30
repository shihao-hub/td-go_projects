package opencode

import (
	"os"
	"path/filepath"
	"testing"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/proxy"
)

func TestQueryDB(t *testing.T) {
	dbPath := filepath.Join(os.Getenv("USERPROFILE"), ".local", "share", "opencode", "opencode.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Skip("opencode.db not found, skipping local test")
	}

	cfg := &config.OpenCodeConfig{
		DBPath: dbPath,
	}
	inj := proxy.NewInjector(os.Stdout)
	titler := NewTitler(cfg, inj)

	// 查询一个已存在的会话
	// ses_e8984926bffeLAdxhRnZJ33B5P 为先前已定位到的会话 ID
	title, found := titler.queryDB("ses_e8984926bffeLAdxhRnZJ33B5P")
	if !found {
		t.Log("Session not found, but queryDB ran without panic")
		return
	}
	t.Logf("Found title: %s", title)
	if title == "" {
		t.Errorf("Expected non-empty title")
	}
}
