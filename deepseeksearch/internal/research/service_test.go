package research

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"deepseeksearch/internal/deepseek"
	"deepseeksearch/internal/storage"
)

type fakeBackend struct {
	search  func(context.Context, deepseek.SearchRequest) (*deepseek.SearchResponse, error)
	suggest func(context.Context, string, string) ([]deepseek.QuerySuggestion, error)
}

func (f fakeBackend) Search(c context.Context, r deepseek.SearchRequest) (*deepseek.SearchResponse, error) {
	return f.search(c, r)
}
func (f fakeBackend) Suggest(c context.Context, m, i string) ([]deepseek.QuerySuggestion, error) {
	return f.suggest(c, m, i)
}

func testStore(t *testing.T) (*storage.Store, string) {
	t.Helper()
	root, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".language_projects")
	}
	root = filepath.Join(root, "language_projects", "deepseeksearch", "dev", "tests")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "research-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "test.db")
	store, err := storage.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func TestResearchLifecycleAndPartialRetry(t *testing.T) {
	ctx := context.Background()
	store, path := testStore(t)
	service := New(store)
	var calls atomic.Int32
	fail := true
	service.NewBackend = func(storage.AppConfig) Backend {
		return fakeBackend{search: func(_ context.Context, r deepseek.SearchRequest) (*deepseek.SearchResponse, error) {
			calls.Add(1)
			if r.OriQuestion != "Go 桌面开发" {
				t.Errorf("缺少原始意图")
			}
			if r.Query == "案例" && fail {
				return nil, errors.New("模拟失败")
			}
			return &deepseek.SearchResponse{Results: []deepseek.SearchResultItem{{Title: r.Query, URL: "https://EXAMPLE.com:443/a#part", RerankScore: 0.9}, {Title: "独立", URL: "https://example.com/b?q=" + r.Query}}}, nil
		}, suggest: func(_ context.Context, model, input string) ([]deepseek.QuerySuggestion, error) {
			var obj map[string]interface{}
			if json.Unmarshal([]byte(input), &obj) != nil {
				t.Error("上下文不是 JSON")
			}
			if model != "deepseek-flash" {
				t.Errorf("模型 %s", model)
			}
			return []deepseek.QuerySuggestion{{Query: "新问题", Reason: "寻找遗漏"}}, nil
		}}
	}
	v, err := service.Create(ctx, "Go 桌面开发")
	if err != nil {
		t.Fatal(err)
	}
	v, err = service.SetDraft(ctx, v.ID, Draft{Count: 10, Queries: []Query{{Query: "官方", Selected: true}, {Query: "案例", Selected: true}, {Query: "留待后续", Selected: false}}})
	if err != nil {
		t.Fatal(err)
	}
	v, err = service.Run(ctx, v.ID, RunOptions{}, nil)
	if err == nil || Wrap(v, err).Error.Code != "partial_failure" {
		t.Fatalf("应部分失败: %v", err)
	}
	if len(v.Results) != 2 || len(v.Draft.Queries) != 1 {
		t.Fatalf("结果/未选草稿丢失: %+v", v)
	}
	fail = false
	v, err = service.Run(ctx, v.ID, RunOptions{RetryFailed: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || len(v.Results) != 3 || len(v.Results[0].Sources) != 2 {
		t.Fatalf("重试范围或融合错误: calls=%d results=%+v", calls.Load(), v.Results)
	}
	v, err = service.AddFeedback(ctx, v.ID, Feedback{Kind: "need_examples", Text: "更多工程案例", ResultID: v.Results[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	v, err = service.Suggest(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Draft.Queries[1].Selected {
		t.Fatal("AI 候选被自动勾选")
	}
	second, err := storage.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	restored, err := New(second).Get(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Feedback) != 1 || len(restored.Rounds) != 2 || len(restored.Results) != 3 || restored.Busy {
		t.Fatal("跨入口恢复失败")
	}
}

func TestCacheContextIsolationAndEmptyResult(t *testing.T) {
	ctx := context.Background()
	store, _ := testStore(t)
	s := New(store)
	var calls atomic.Int32
	s.NewBackend = func(storage.AppConfig) Backend {
		return fakeBackend{search: func(context.Context, deepseek.SearchRequest) (*deepseek.SearchResponse, error) {
			calls.Add(1)
			return &deepseek.SearchResponse{Results: []deepseek.SearchResultItem{}}, nil
		}}
	}
	run := func(question, locale string, content, refresh bool) {
		t.Helper()
		v, e := s.Create(ctx, question)
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.SetDraft(ctx, v.ID, Draft{Count: 10, Localization: locale, NeedContent: content, Queries: []Query{{Query: "相同词", Selected: true}}})
		if e != nil {
			t.Fatal(e)
		}
		v, e = s.Run(ctx, v.ID, RunOptions{Refresh: refresh}, nil)
		if e != nil {
			t.Fatal(e)
		}
		if len(v.Results) != 0 {
			t.Fatal("空结果非成功")
		}
	}
	run("意图一", "", false, false)
	run("意图一", "", false, false)
	if calls.Load() != 1 {
		t.Fatal("未复用空结果缓存")
	}
	run("意图二", "", false, false)
	run("意图一", "en-US", false, false)
	run("意图一", "", true, false)
	run("意图一", "", false, true)
	if calls.Load() != 5 {
		t.Fatalf("上下文缓存混用: %d", calls.Load())
	}
}

func TestCancelAndConcurrentWriter(t *testing.T) {
	store, path := testStore(t)
	s := New(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	var once sync.Once
	s.NewBackend = func(storage.AppConfig) Backend {
		return fakeBackend{search: func(ctx context.Context, _ deepseek.SearchRequest) (*deepseek.SearchResponse, error) {
			once.Do(func() { close(started) })
			<-ctx.Done()
			return nil, ctx.Err()
		}}
	}
	v, err := s.Create(ctx, "取消测试")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, e := s.Run(ctx, v.ID, RunOptions{}, nil); done <- e }()
	<-started
	second, err := storage.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, e := New(second).AddFeedback(context.Background(), v.ID, Feedback{Kind: "too_broad"}); !errors.Is(e, storage.ErrResearchBusy) {
		t.Fatalf("没有跨进程互斥: %v", e)
	}
	cancel()
	if e := <-done; e == nil {
		t.Fatal("取消误报成功")
	}
	restored, e := s.Get(context.Background(), v.ID)
	if e != nil {
		t.Fatal(e)
	}
	if restored.Busy || restored.Rounds[0].Status != "interrupted" {
		t.Fatal("取消未保存/释放租约")
	}
}

func TestDraftConflictAndMergeBoundaries(t *testing.T) {
	store, _ := testStore(t)
	s := New(store)
	ctx := context.Background()
	v, e := s.Create(ctx, "问题")
	if e != nil {
		t.Fatal(e)
	}
	draft := v.Draft
	draft.Revision = v.Revision
	v, e = s.SetDraft(ctx, v.ID, draft)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetDraft(ctx, v.ID, draft); e == nil {
		t.Fatal("旧版本覆盖了新草稿")
	}
	if _, e = s.SetDraft(ctx, v.ID, Draft{Count: 51}); e == nil {
		t.Fatal("接受非法条数")
	}
	if canonical("https://EXAMPLE.com:443/a?x=1#f") != "https://example.com/a?x=1" {
		t.Fatal("URL 规范化错误")
	}
	if canonical("https://example.com/a?x=1") == canonical("https://example.com/a?x=2") {
		t.Fatal("误删查询参数")
	}
}

func TestMultiQueryConcurrencyAndCancellationKeepsResults(t *testing.T) {
	store, _ := testStore(t)
	s := New(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 6)
	release := make(chan struct{})
	var active, maxActive atomic.Int32
	s.NewBackend = func(storage.AppConfig) Backend {
		return fakeBackend{search: func(ctx context.Context, req deepseek.SearchRequest) (*deepseek.SearchResponse, error) {
			n := active.Add(1)
			defer active.Add(-1)
			for {
				old := maxActive.Load()
				if n <= old || maxActive.CompareAndSwap(old, n) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &deepseek.SearchResponse{Results: []deepseek.SearchResultItem{{Title: req.Query, URL: "https://example.com/" + req.Query}}}, nil
		}}
	}
	v, e := s.Create(ctx, "并发测试")
	if e != nil {
		t.Fatal(e)
	}
	queries := []Query{}
	for _, q := range []string{"a", "b", "c", "d", "e", "f"} {
		queries = append(queries, Query{Query: q, Selected: true})
	}
	if _, e = s.SetDraft(ctx, v.ID, Draft{Count: 10, Queries: queries}); e != nil {
		t.Fatal(e)
	}
	type answer struct {
		v *Session
		e error
	}
	done := make(chan answer, 1)
	go func() {
		v, e := s.Run(ctx, v.ID, RunOptions{}, func(v *Session) {
			if len(v.Results) > 0 {
				cancel()
			}
		})
		done <- answer{v, e}
	}()
	for range 3 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("未启动三路")
		}
	}
	select {
	case <-started:
		t.Fatal("并发超过三路")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	out := <-done
	if out.e == nil || len(out.v.Results) == 0 || out.v.Busy {
		t.Fatalf("取消丢失已完成结果: %v %+v", out.e, out.v)
	}
	if maxActive.Load() > 3 {
		t.Fatal("并发超限")
	}
	loaded, e := s.Get(context.Background(), v.ID)
	if e != nil || len(loaded.Results) == 0 {
		t.Fatal("已完成结果未持久化")
	}
}
