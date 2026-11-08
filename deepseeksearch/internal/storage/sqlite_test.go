package storage

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
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

	// 缓存未命中测试 (过期)：定点写入 2 秒前的记录（避开同秒 >= 边界竞态），
	// 0 时长窗口必须判定失效
	staleAt := time.Now().UTC().Add(-2 * time.Second).Format(dbTimeLayout)
	if _, err := store.db.Exec(`
		INSERT INTO search_history(query, count, need_content, result_count, results_json, created_at)
		VALUES (?, ?, 0, 1, ?, ?)`, "过期查询", 10, `[{"title":"过期结果","url":"https://example.com/stale"}]`, staleAt); err != nil {
		t.Fatalf("写入过期搜索记录失败: %v", err)
	}
	expired, err := store.GetCachedSearch("过期查询", 5, false, 0)
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

// 缓存时效窗口必须贴着 UTC 时间线：
// CURRENT_TIMESTAMP 写的是 UTC 墙钟，若 cutoff 侧混入本地时区（如 UTC+8），
// 24h 窗口会漂移成 16~32h——23 小时前的记录（窗口内）会被提前判死。
func TestSearchCacheUTCWindow(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "tz.db"))
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer store.Close()

	insertSearchAt := func(t *testing.T, query, createdAtUTC string) {
		t.Helper()
		res := `[{"title":"示例结果","url":"https://example.com/a"},{"title":"示例结果2","url":"https://example.com/b"}]`
		if _, err := store.db.Exec(`
			INSERT INTO search_history(query, count, need_content, result_count, results_json, created_at)
			VALUES (?, ?, 0, 2, ?, ?)`, query, 2, res, createdAtUTC); err != nil {
			t.Fatalf("写入定点搜索记录失败: %v", err)
		}
	}

	inside := time.Now().UTC().Add(-23 * time.Hour).Format(dbTimeLayout)
	insertSearchAt(t, "谷氨酸棒杆菌", inside)

	cached, err := store.GetCachedSearch("谷氨酸棒杆菌", 2, false, 24*time.Hour)
	if err != nil {
		t.Fatalf("读取缓存失败: %v", err)
	}
	if cached == nil {
		t.Fatalf("24h 窗口内（23h 前）的记录必须命中，但缓存为空；UTC+偏移时区下实况 created_at=%s", inside)
	}
	// 回读的时间必须解析为 UTC 时刻本身，而非被本地时区再偏移一次
	if got := cached.CreatedAt.UTC().Format(dbTimeLayout); got != inside {
		t.Errorf("回读时间解析错误: got=%s want=%s", got, inside)
	}

	outside := time.Now().UTC().Add(-25 * time.Hour).Format(dbTimeLayout)
	insertSearchAt(t, "过期窗口记录", outside)
	expired, err := store.GetCachedSearch("过期窗口记录", 2, false, 24*time.Hour)
	if err != nil {
		t.Fatalf("读取过期缓存报错: %v", err)
	}
	if expired != nil {
		t.Errorf("25h 前的记录（超出 24h 窗口）不应命中: %+v", expired)
	}
}

// 宽缓存截断：历史记录允许 count 更宽，命中后结果列表与 ResultCount
// 必须截到本次请求条数，保证“返回结果数 == 请求条数”的契约。
func TestCachedSearchTruncatesToRequestedCount(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "wide.db"))
	if err != nil {
		t.Fatalf("创建数据库失败: %v", err)
	}
	defer store.Close()

	items := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		items = append(items, fmt.Sprintf(`{"title":"结果%d","url":"https://example.com/%d"}`, i, i))
	}
	if _, err := store.db.Exec(`
		INSERT INTO search_history(query, count, need_content, result_count, results_json, created_at)
		VALUES (?, 50, 0, 3, ?, CURRENT_TIMESTAMP)`, "宽缓存查询", "["+strings.Join(items, ",")+"]"); err != nil {
		t.Fatalf("写入宽缓存记录失败: %v", err)
	}

	cached, err := store.GetCachedSearch("宽缓存查询", 2, false, 24*time.Hour)
	if err != nil {
		t.Fatalf("读取缓存失败: %v", err)
	}
	if cached == nil {
		t.Fatalf("预期命中缓存")
	}

	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(cached.ResultsJSON), &got); err != nil {
		t.Fatalf("截断后的 ResultsJSON 不是合法 JSON: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("请求 2 条应截断为 2 条，实际返回 %d 条", len(got))
	}
	if cached.ResultCount != 2 {
		t.Errorf("ResultCount 应同步为截断后的 2，实际为 %d", cached.ResultCount)
	}

	full, err := store.GetCachedSearch("宽缓存查询", 50, false, 24*time.Hour)
	if err != nil {
		t.Fatalf("按 50 条读取缓存失败: %v", err)
	}
	if full == nil {
		t.Fatalf("count=50 时也应命中同一条宽缓存记录")
	}
	var fullItems []map[string]interface{}
	if err := json.Unmarshal([]byte(full.ResultsJSON), &fullItems); err != nil {
		t.Fatalf("ResultsJSON 解析失败: %v", err)
	}
	if len(fullItems) != 3 {
		t.Errorf("请求 50 条（记录仅 3 条）应原样返回全部，实际 %d 条", len(fullItems))
	}
	if full.ResultCount != 3 {
		t.Errorf("无需截断时 ResultCount 应保持 3，实际为 %d", full.ResultCount)
	}
}

func TestParseDBTime(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Time
	}{
		{"CURRENT_TIMESTAMP 墙钟", "2025-06-01 08:30:00", time.Date(2025, 6, 1, 8, 30, 0, 0, time.UTC)},
		{"历史 RFC3339 兼容", "2025-06-01T08:30:00Z", time.Date(2025, 6, 1, 8, 30, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDBTime(tc.in)
			if err != nil {
				t.Fatalf("解析 %q 失败: %v", tc.in, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("解析 %q = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
	if _, err := parseDBTime("不是时间"); err == nil {
		t.Errorf("非法时间串应返回错误")
	}
}
