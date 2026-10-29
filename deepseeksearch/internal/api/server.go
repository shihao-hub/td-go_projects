package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"deepseeksearch/internal/deepseek"
	"deepseeksearch/internal/storage"
	"deepseeksearch/internal/web"
)

type Server struct {
	store *storage.Store
	meta  Meta
}

type Meta struct {
	Channel   string `json:"channel"`
	BuildMode string `json:"buildMode"`
	Version   string `json:"version"`
	DataDir   string `json:"dataDir"`
	DBPath    string `json:"dbPath"`
}

func NewServer(store *storage.Store, meta Meta) *Server {
	return &Server{
		store: store,
		meta:  meta,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("POST /api/config", s.handleSaveConfig)
	mux.HandleFunc("POST /api/config/test", s.handleTestConfig)
	mux.HandleFunc("POST /api/search", s.handleSearch)
	mux.HandleFunc("POST /api/fetch", s.handleFetch)
	mux.HandleFunc("GET /api/history/searches", s.handleListSearchHistory)
	mux.HandleFunc("GET /api/history/fetches", s.handleListFetchHistory)

	subFS, err := fs.Sub(web.Assets, "assets")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(subFS)))
	}

	return mux
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.meta)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取配置失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var cfg storage.AppConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "解析参数失败: "+err.Error())
		return
	}

	if err := s.store.SaveConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "保存配置失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type testConfigRequest struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl"`
}

func (s *Server) handleTestConfig(w http.ResponseWriter, r *http.Request) {
	var req testConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	client := deepseek.NewClient(req.APIKey, req.BaseURL)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if err := client.TestAuth(ctx); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "连通正常（免费端点验证通过）"})
}

type searchApiRequest struct {
	Query        string `json:"query"`
	Count        int    `json:"count"`
	OriQuestion  string `json:"ori_question"`
	NeedContent  bool   `json:"need_content"`
	Localization string `json:"localization"`
	Refresh      bool   `json:"refresh"` // 是否强制刷新不走本地缓存
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req searchApiRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "参数解析失败: "+err.Error())
		return
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "搜索关键词不能为空")
		return
	}
	if req.Count <= 0 {
		req.Count = 10
	}
	if req.Count > 50 {
		req.Count = 50
	}

	// 1. 若非强制刷新，优先检查 24 小时内的本地搜索缓存（0 元消耗）
	if !req.Refresh {
		if cached, _ := s.store.GetCachedSearch(req.Query, req.Count, req.NeedContent, 24*time.Hour); cached != nil && cached.ResultsJSON != "" {
			var items []deepseek.SearchResultItem
			if err := json.Unmarshal([]byte(cached.ResultsJSON), &items); err == nil && len(items) > 0 {
				writeJSON(w, http.StatusOK, deepseek.SearchResponse{
					Results:   items,
					FromCache: true,
					CachedAt:  &cached.CreatedAt,
				})
				return
			}
		}
	}

	cfg, err := s.store.GetConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取配置失败: "+err.Error())
		return
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		writeError(w, http.StatusUnauthorized, "尚未配置 DeepSeek API Key，请在设置中保存")
		return
	}

	client := deepseek.NewClient(cfg.APIKey, cfg.BaseURL)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	resp, err := client.Search(ctx, deepseek.SearchRequest{
		Query:        req.Query,
		Count:        req.Count,
		OriQuestion:  req.OriQuestion,
		NeedContent:  req.NeedContent,
		Localization: req.Localization,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 记录搜索历史与缓存
	_, _ = s.store.AddSearchHistory(req.Query, req.Count, req.NeedContent, len(resp.Results), resp.Results)

	writeJSON(w, http.StatusOK, resp)
}

type fetchApiRequest struct {
	URL     string `json:"url"`
	Refresh bool   `json:"refresh"` // 是否强制刷新不走本地缓存
}

func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	var req fetchApiRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "参数解析失败: "+err.Error())
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "网页 URL 不能为空")
		return
	}

	// 1. 若非强制刷新，优先检查 7 天内的本地网页抓取缓存（0 元消耗）
	if !req.Refresh {
		if cached, _ := s.store.GetCachedFetch(req.URL, 7*24*time.Hour); cached != nil && cached.Content != "" {
			writeJSON(w, http.StatusOK, deepseek.FetchResponse{
				Title:     cached.Title,
				Content:   cached.Content,
				FromCache: true,
				CachedAt:  &cached.FetchedAt,
			})
			return
		}
	}

	cfg, err := s.store.GetConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取配置失败: "+err.Error())
		return
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		writeError(w, http.StatusUnauthorized, "尚未配置 DeepSeek API Key，请在设置中保存")
		return
	}

	client := deepseek.NewClient(cfg.APIKey, cfg.BaseURL)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	resp, err := client.Fetch(ctx, deepseek.FetchRequest{URL: req.URL})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 记录抓取历史与缓存
	_, _ = s.store.AddFetchHistory(req.URL, resp.Title, resp.Content)

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleListSearchHistory(w http.ResponseWriter, r *http.Request) {
	history, err := s.store.ListSearchHistory(30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取搜索历史失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) handleListFetchHistory(w http.ResponseWriter, r *http.Request) {
	history, err := s.store.ListFetchHistory(30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取抓取历史失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// Listen 在回环地址上绑定监听器；port 传 0 表示动态探测可用端口
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
}

// URL 由监听器推导出实际可访问地址
func URL(listener net.Listener) string {
	return "http://" + listener.Addr().String()
}

// Serve 在调用方已绑定的监听器上阻塞提供服务
func (s *Server) Serve(listener net.Listener) error {
	return http.Serve(listener, s.Handler())
}

// Start 绑定端口并提供服务，可选自动打开浏览器
func (s *Server) Start(port int, openBrowser bool) (string, error) {
	listener, err := Listen(port)
	if err != nil {
		return "", fmt.Errorf("绑定监听失败: %w", err)
	}
	url := URL(listener)

	if openBrowser {
		go openURLInBrowser(url)
	}

	return url, s.Serve(listener)
}

func openURLInBrowser(url string) {
	time.Sleep(100 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
