package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStorageCache(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer store.Close()

	// 1. 测试搜索缓存写入与读取
	query := "golang testing"
	results := []map[string]interface{}{
		{"title": "Go 官方测试", "url": "https://golang.org/pkg/testing"},
	}

	_, err = store.AddSearchHistory(query, 10, false, 1, results)
	if err != nil {
		t.Fatalf("添加搜索历史失败: %v", err)
	}

	// 缓存命中测试 (24 小时内)
	cached, err := store.GetCachedSearch(query, 5, false, 24*time.Hour)
	if err != nil {
		t.Fatalf("读取搜索缓存失败: %v", err)
	}
	if cached == nil {
		t.Fatalf("预期命中搜索缓存，但返回 nil")
	}
	if cached.ResultCount != 1 {
		t.Errorf("预期结果数量为 1，实际为 %d", cached.ResultCount)
	}

	// 缓存未命中测试 (过期)
	expired, err := store.GetCachedSearch(query, 5, false, 0)
	if err != nil {
		t.Fatalf("读取过期缓存报错: %v", err)
	}
	if expired != nil {
		t.Errorf("预期 0 时长缓存判定过期，但命中: %v", expired)
	}

	// 2. 测试 Fetch 网页抓取缓存
	url := "https://example.com/test-article"
	title := "测试文章标题"
	content := "# 这是一个测试正文"

	_, err = store.AddFetchHistory(url, title, content)
	if err != nil {
		t.Fatalf("添加抓取历史失败: %v", err)
	}

	fetchCached, err := store.GetCachedFetch(url, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("读取抓取缓存失败: %v", err)
	}
	if fetchCached == nil {
		t.Fatalf("预期命中网页抓取缓存，但返回 nil")
	}
	if fetchCached.Title != title || fetchCached.Content != content {
		t.Errorf("抓取缓存数据不一致: %+v", fetchCached)
	}
}
