package research

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"deepseeksearch/internal/deepseek"
	"deepseeksearch/internal/storage"
)

type Backend interface {
	Search(context.Context, deepseek.SearchRequest) (*deepseek.SearchResponse, error)
	Suggest(context.Context, string, string) ([]deepseek.QuerySuggestion, error)
}
type Service struct {
	store      *storage.Store
	NewBackend func(storage.AppConfig) Backend
}

func New(store *storage.Store) *Service {
	return &Service{store: store, NewBackend: func(c storage.AppConfig) Backend { return deepseek.NewClient(c.APIKey, c.BaseURL) }}
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func (s *Service) List(ctx context.Context) ([]storage.ResearchDocument, error) {
	return s.store.ListResearch(ctx)
}
func (s *Service) Create(ctx context.Context, question string) (*Session, error) {
	question = strings.TrimSpace(question)
	if question == "" || len(question) > 4000 {
		return nil, invalid("原始问题不能为空且不能超过 4000 字节")
	}
	v := &Session{ID: newID(), Question: question, Revision: 1, UpdatedAt: now(), Draft: Draft{Count: DefaultCount, Queries: []Query{{ID: newID(), Query: question, Reason: "原始问题", Selected: true}}}, Rounds: []Round{}, Feedback: []Feedback{}, Results: []Result{}}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if err := s.store.CreateResearch(ctx, v.ID, question, string(raw)); err != nil {
		return nil, err
	}
	return v, nil
}
func (s *Service) Get(ctx context.Context, id string) (*Session, error) {
	d, err := s.store.GetResearch(ctx, id)
	if err != nil {
		return nil, err
	}
	var v Session
	if err := json.Unmarshal([]byte(d.Data), &v); err != nil {
		return nil, errors.New("研究记录无法解析")
	}
	v.Revision = d.Revision
	v.UpdatedAt = d.UpdatedAt
	v.Busy = d.LeaseUntil > time.Now().Unix()
	if !v.Busy {
		for i := range v.Rounds {
			if v.Rounds[i].Status == "running" {
				v.Rounds[i].Status = "interrupted"
				for j := range v.Rounds[i].Executions {
					e := &v.Rounds[i].Executions[j]
					if e.Status == "running" || e.Status == "pending" {
						e.Status = "interrupted"
						e.Error = "操作中断，可手动重试"
					}
				}
			}
		}
	}
	v.Results = merge(v.Rounds)
	return &v, nil
}
func (s *Service) locked(ctx context.Context, id string, fn func(*Session, string) error) (out *Session, err error) {
	token := newID()
	if err = s.store.ClaimResearch(ctx, id, token); err != nil {
		return nil, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if releaseErr := s.store.ReleaseResearch(cleanup, id, token); releaseErr != nil && err == nil {
			err = releaseErr
		}
		if out != nil {
			out.Busy = false
		}
	}()
	out, err = s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// 取得过期租约后将上次未完成的状态确定为中断。
	for i := range out.Rounds {
		r := &out.Rounds[i]
		if r.Status == "running" {
			r.Status = "interrupted"
			for j := range r.Executions {
				if r.Executions[j].Status == "running" || r.Executions[j].Status == "pending" {
					r.Executions[j].Status = "interrupted"
				}
			}
		}
	}
	err = fn(out, token)
	out.Results = merge(out.Rounds)
	return out, err
}
func (s *Service) save(ctx context.Context, v *Session, token string) error {
	v.UpdatedAt = now()
	v.Revision++
	v.Results = nil // 结果由不可变的轮次快照投影，避免重复落盘。
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.store.SaveResearch(ctx, v.ID, token, string(raw))
}
func normalize(d *Draft) error {
	if d.Queries == nil {
		return invalid("queries 必须提供数组，可以为空数组")
	}
	if d.Count == 0 {
		d.Count = DefaultCount
	}
	if d.Count < 1 || d.Count > 50 {
		return invalid("每路条数须为 1–50")
	}
	if len(d.Queries) > MaxQueries {
		return invalid("每轮最多 6 条查询")
	}
	if len(d.Localization) > 100 {
		return invalid("地域参数过长")
	}
	seen := map[string]bool{}
	seenIDs := map[string]bool{}
	for i := range d.Queries {
		q := &d.Queries[i]
		q.Query = strings.TrimSpace(q.Query)
		if q.Query == "" || len(q.Query) > 4000 || len(q.Reason) > 2000 {
			return invalid("查询不能为空且长度不能超限")
		}
		if seen[q.Query] {
			return invalid("同一草稿不能包含重复查询")
		}
		seen[q.Query] = true
		if q.ID == "" {
			q.ID = newID()
		}
		if len(q.ID) > 64 || seenIDs[q.ID] {
			return invalid("查询 ID 过长或重复")
		}
		seenIDs[q.ID] = true
	}
	return nil
}
func (s *Service) SetDraft(ctx context.Context, id string, d Draft) (*Session, error) {
	if err := normalize(&d); err != nil {
		return nil, err
	}
	return s.locked(ctx, id, func(v *Session, token string) error {
		if d.Revision != 0 && d.Revision != v.Revision {
			return &Error{Code: "conflict", Message: "研究已被另一入口更新，请重新加载"}
		}
		v.Draft = d
		v.Draft.Revision = 0
		return s.save(ctx, v, token)
	})
}
func (s *Service) AddFeedback(ctx context.Context, id string, f Feedback) (*Session, error) {
	switch f.Kind {
	case "too_broad", "off_topic", "need_examples", "need_counterexamples", "custom":
	default:
		return nil, invalid("反馈类型无效")
	}
	f.Text = strings.TrimSpace(f.Text)
	if len(f.Text) > 4000 || (f.Kind == "custom" && f.Text == "") {
		return nil, invalid("请填写不超过 4000 字节的反馈")
	}
	return s.locked(ctx, id, func(v *Session, token string) error {
		if len(v.Feedback) >= 200 {
			return invalid("反馈已达上限，请新建研究")
		}
		if f.ResultID != "" {
			found := false
			for _, r := range v.Results {
				if r.ID == f.ResultID {
					found = true
				}
			}
			if !found {
				return invalid("反馈对应的结果不存在")
			}
		}
		f.CreatedAt = now()
		v.Feedback = append(v.Feedback, f)
		return s.save(ctx, v, token)
	})
}
func (s *Service) Suggest(ctx context.Context, id string) (*Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return s.locked(ctx, id, func(v *Session, token string) error {
		cfg, err := s.store.GetConfig()
		if err != nil {
			return err
		}
		// 上下文有界，结果只传标题/片段，不上传正文或凭据。
		summaries := []map[string]string{}
		for i, r := range v.Results {
			if i >= 12 {
				break
			}
			snippet := []rune(r.Snippet)
			if len(snippet) > 300 {
				snippet = snippet[:300]
			}
			summaries = append(summaries, map[string]string{"id": r.ID, "title": r.Title, "snippet": string(snippet)})
		}
		queries := []string{}
		for _, r := range v.Rounds {
			for _, e := range r.Executions {
				queries = append(queries, e.Query.Query)
			}
		}
		if len(queries) > 36 {
			queries = queries[len(queries)-36:]
		}
		feedback := v.Feedback
		if len(feedback) > 12 {
			feedback = feedback[len(feedback)-12:]
		}
		raw, _ := json.Marshal(map[string]interface{}{"question": v.Question, "draft": v.Draft.Queries, "searchedQueries": queries, "feedback": feedback, "results": summaries})
		suggestions, err := s.NewBackend(cfg).Suggest(ctx, cfg.AIModel, string(raw))
		if err != nil {
			return &Error{Code: "model_failed", Message: "建议生成失败，请检查模型配置后重试；草稿已保留"}
		}
		// 首次保留原词，后续沿用手工草稿；建议只追加到剩余空位。
		seen := map[string]bool{}
		for _, q := range v.Draft.Queries {
			seen[q.Query] = true
		}
		for _, r := range v.Rounds {
			for _, e := range r.Executions {
				if e.Status == "success" {
					seen[e.Query.Query] = true
				}
			}
		}
		added := 0
		for _, q := range suggestions {
			if len(v.Draft.Queries) >= MaxQueries {
				break
			}
			if !seen[q.Query] {
				v.Draft.Queries = append(v.Draft.Queries, Query{ID: newID(), Query: q.Query, Reason: q.Reason})
				seen[q.Query] = true
				added++
			}
		}
		if added == 0 {
			return invalid("没有可追加的新建议，请删除不需要的草稿查询后重试")
		}
		return s.save(ctx, v, token)
	})
}

func cacheKey(cfg storage.AppConfig, req deepseek.SearchRequest) string {
	raw, _ := json.Marshal(struct {
		BaseURL string
		Request deepseek.SearchRequest
	}{strings.TrimRight(cfg.BaseURL, "/"), req})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Run 在请求生命周期内执行。只由当前调用者持有租约，不创建脱离请求的后台任务。
func (s *Service) Run(ctx context.Context, id string, opt RunOptions, progress func(*Session)) (*Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	return s.locked(ctx, id, func(v *Session, token string) error {
		if len(v.Rounds) >= 100 {
			return invalid("研究已达 100 轮，请新建研究")
		}
		draft := v.Draft
		if opt.RetryFailed {
			if len(v.Rounds) == 0 {
				return invalid("没有可以重试的轮次")
			}
			last := v.Rounds[len(v.Rounds)-1]
			draft = Draft{Count: last.Count, NeedContent: last.NeedContent, Localization: last.Localization}
			for _, e := range last.Executions {
				if e.Status != "success" {
					q := e.Query
					q.Selected = true
					draft.Queries = append(draft.Queries, q)
				}
			}
		}
		if err := normalize(&draft); err != nil {
			return err
		}
		round := Round{Number: len(v.Rounds) + 1, Status: "running", StartedAt: now(), Count: draft.Count, NeedContent: draft.NeedContent, Localization: draft.Localization, Executions: []Execution{}}
		for _, q := range draft.Queries {
			if q.Selected {
				round.Executions = append(round.Executions, Execution{Query: q, Status: "pending", Items: []deepseek.SearchResultItem{}})
			}
		}
		if len(round.Executions) == 0 {
			return invalid("请至少选择一条查询")
		}
		cfg, err := s.store.GetConfig()
		if err != nil {
			return err
		}
		backend := s.NewBackend(cfg)
		v.Rounds = append(v.Rounds, round)
		index := len(v.Rounds) - 1
		if err := s.save(ctx, v, token); err != nil {
			return err
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		gate := make(chan struct{}, 3)
		var saveErr error
		for i := range round.Executions {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				select {
				case gate <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-gate }()
				if ctx.Err() != nil {
					return
				}
				mu.Lock()
				v.Rounds[index].Executions[i].Status = "running"
				err := s.save(ctx, v, token)
				if err != nil {
					saveErr = err
					cancel()
				}
				mu.Unlock()
				if err != nil {
					return
				}
				req := deepseek.SearchRequest{Query: round.Executions[i].Query.Query, Count: draft.Count, OriQuestion: v.Question, NeedContent: draft.NeedContent, Localization: draft.Localization}
				routeCtx, routeCancel := context.WithTimeout(ctx, 60*time.Second)
				defer routeCancel()
				var response *deepseek.SearchResponse
				var routeErr error
				cached := false
				key := cacheKey(cfg, req)
				if !opt.Refresh {
					raw, cacheErr := s.store.GetResearchCache(routeCtx, key)
					if cacheErr == nil && raw != "" {
						var r deepseek.SearchResponse
						if json.Unmarshal([]byte(raw), &r) == nil {
							response = &r
							cached = true
						}
					}
				}
				if response == nil {
					response, routeErr = backend.Search(routeCtx, req)
					if routeErr == nil && response != nil {
						raw, _ := json.Marshal(response)
						// 缓存写入失败不能丢弃已取得的有效搜索结果。
						if cacheErr := s.store.PutResearchCache(routeCtx, key, string(raw)); cacheErr != nil {
							mu.Lock()
							v.Rounds[index].Executions[i].Error = "缓存未写入，搜索结果仍保存在本轮记录中"
							mu.Unlock()
						}
					}
				}
				mu.Lock()
				defer mu.Unlock()
				e := &v.Rounds[index].Executions[i]
				e.FromCache = cached
				if routeErr != nil || response == nil {
					e.Status = "failed"
					e.Error = "搜索失败，可手动重试"
					if routeCtx.Err() != nil {
						e.Error = "搜索超时或已取消"
					}
				} else {
					e.Status = "success"
					e.Items = response.Results
					if e.Items == nil {
						e.Items = []deepseek.SearchResultItem{}
					}
				}
				cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
				defer done()
				if err := s.save(cleanup, v, token); err != nil {
					saveErr = err
					cancel()
				}
				if progress != nil {
					snapshot := *v
					snapshot.Results = merge(v.Rounds)
					progress(&snapshot)
				}
			}(i)
		}
		wg.Wait()
		failed := false
		for i := range v.Rounds[index].Executions {
			e := &v.Rounds[index].Executions[i]
			if e.Status == "pending" || e.Status == "running" {
				e.Status = "interrupted"
				e.Error = "执行中断，可手动重试"
			}
			if e.Status != "success" {
				failed = true
			}
		}
		v.Rounds[index].Status = "success"
		if failed {
			v.Rounds[index].Status = "partial"
		}
		if ctx.Err() != nil {
			v.Rounds[index].Status = "interrupted"
		}
		// 已执行的词留在轮次历史，新草稿为空，方便下一轮补搜。
		if !opt.RetryFailed {
			remaining := []Query{}
			for _, q := range v.Draft.Queries {
				if !q.Selected {
					remaining = append(remaining, q)
				}
			}
			v.Draft.Queries = remaining
		}
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := s.save(cleanup, v, token); err != nil {
			return err
		}
		if saveErr != nil {
			return saveErr
		}
		if failed || ctx.Err() != nil {
			return &Error{Code: "partial_failure", Message: "部分查询未完成，已有结果已保存，可仅重试失败项"}
		}
		return nil
	})
}

func canonical(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return raw
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		host := u.Hostname()
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		u.Host = host
	}
	return u.String()
}
func merge(rounds []Round) []Result {
	result := []Result{}
	lookup := map[string]int{}
	contributions := map[string]map[string]float64{}
	for _, round := range rounds {
		for _, e := range round.Executions {
			if e.Status != "success" {
				continue
			}
			seen := map[string]bool{}
			for rank, item := range e.Items {
				key := canonical(item.URL)
				if key == "" || seen[key] {
					continue
				}
				seen[key] = true
				index, ok := lookup[key]
				if !ok {
					hash := sha256.Sum256([]byte(key))
					index = len(result)
					lookup[key] = index
					result = append(result, Result{SearchResultItem: item, ID: fmt.Sprintf("%x", hash[:12]), FirstRound: round.Number, Sources: []Source{}})
					contributions[key] = map[string]float64{}
				}
				r := &result[index]
				if r.Title == "" {
					r.Title = item.Title
				}
				if item.Content != "" {
					r.Content = item.Content
				}
				r.Sources = append(r.Sources, Source{Round: round.Number, QueryID: e.ID, Query: e.Query.Query, Rank: rank + 1, Score: item.RerankScore})
				weight := 1 / float64(60+rank+1)
				if weight > contributions[key][e.Query.Query] {
					r.FusionScore += weight - contributions[key][e.Query.Query]
					contributions[key][e.Query.Query] = weight
				}
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].FusionScore > result[j].FusionScore })
	return result
}
