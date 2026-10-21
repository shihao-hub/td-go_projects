// Package webui 是 GUI 与 CLI serve 共用的内嵌 WebUI HTTP 层：
// 静态资源 + 配额查询 API，直接装配 service，不依赖 Wails 桌面壳。
package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"agyquota/internal/api"
	"agyquota/internal/appdata"
	"agyquota/internal/buildinfo"
	"agyquota/internal/service"
	"agyquota/internal/web"
)

const quotaRequestTimeout = 2 * time.Minute

// Logf 是诊断日志注入点：GUI 传落盘日志，daemon 传标准 logger，nil 静默。
type Logf func(format string, args ...any)

// sourceInfo 是 WebUI 数据源选择项（与 internal/api 的 source 枚举同源）。
type sourceInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// metaResponse 是 WebUI 启动时读取的运行时元信息。
type metaResponse struct {
	Name      string       `json:"name"`
	Version   string       `json:"version"`
	BuildMode string       `json:"buildMode"`
	Channel   string       `json:"channel"`
	DataDir   string       `json:"dataDir"`
	Sources   []sourceInfo `json:"sources"`
}

// Server 是只读 HTTP 层：静态 WebUI + 配额查询。
type Server struct {
	svc   *service.Service
	cache *quotaCache
	logf  Logf
}

// New 构造 WebUI 服务；svc 为空时自建，logf 为空时丢弃日志。
func New(svc *service.Service, logf Logf) *Server {
	if svc == nil {
		svc = service.New()
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{svc: svc, cache: newQuotaCache(logf), logf: logf}
}

// Handler 返回静态资源 + /api/* 的完整路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/cache", s.handleCachedQuota)
	mux.HandleFunc("GET /api/quota", s.handleQuota)

	assets, err := fs.Sub(web.Assets, "assets")
	if err != nil {
		s.logf("加载内嵌 WebUI 失败: %v", err)
	} else {
		mux.Handle("/", http.FileServer(http.FS(assets)))
	}
	return mux
}

func (s *Server) handleMeta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, metaResponse{
		Name:      "agyquota",
		Version:   buildinfo.Version,
		BuildMode: buildinfo.BuildMode,
		Channel:   appdata.Channel(),
		DataDir:   appdata.Dir(),
		Sources: []sourceInfo{
			{ID: api.SourceAgy, Label: "Antigravity CLI", Description: "调用本机 agy 查询终端与桌面端账号，执行后立即回收进程"},
			{ID: api.SourceZed, Label: "Zed 插件", Description: "读取 Zed antigravity-acp 凭据直连 Google，带本地安全缓存"},
		},
	})
}

// handleQuota 执行一次配额查询。
//   - source=agy|zed（缺省 agy）
//   - raw=1 时返回接口原始响应（排障用）
func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = api.SourceAgy
	}
	if source != api.SourceAgy && source != api.SourceZed {
		w.WriteHeader(http.StatusBadRequest)
		_ = api.WriteErr(w, &api.Error{
			Code:    api.ErrBadArgs,
			Message: fmt.Sprintf("未知数据源: %s", source),
		})
		return
	}
	raw := r.URL.Query().Get("raw") == "1"

	ctx, cancel := context.WithTimeout(r.Context(), quotaRequestTimeout)
	defer cancel()

	start := time.Now()
	if raw {
		data, err := s.svc.FetchRaw(ctx, service.Options{Source: source}, nil)
		if err != nil {
			s.writeEnvelopeError(w, err)
			return
		}
		s.writeOK(w, api.RawResponse{Raw: data})
		return
	}

	snap, err := s.svc.GetQuota(ctx, service.Options{Source: source}, nil)
	if err != nil {
		s.writeEnvelopeError(w, err)
		return
	}
	s.cache.put(snap)
	s.logf("WebUI 配额查询完成: source=%s 耗时=%s", source, time.Since(start).Round(time.Millisecond))
	s.writeOK(w, snap)
}

// handleCachedQuota 返回上次成功查询的缓存快照（没有则 data=null）。
// 桌面壳启动时只读缓存，不发起任何查询。
func (s *Server) handleCachedQuota(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = api.SourceAgy
	}
	snap, ok := s.cache.get(source)
	if !ok {
		s.writeOK(w, nil)
		return
	}
	s.writeOK(w, snap)
}

// writeOK 写成功包络并附带 no-store（配额数据时效性强）。
func (s *Server) writeOK(w http.ResponseWriter, data any) {
	w.Header().Set("Cache-Control", "no-store")
	_ = api.WriteOK(w, data)
}

// writeEnvelopeError 把业务错误统一转成失败包络；HTTP 状态保持 200，
// 由前端按 ok 字段分支（与 daemon 契约一致）。
func (s *Server) writeEnvelopeError(w http.ResponseWriter, err error) {
	w.Header().Set("Cache-Control", "no-store")
	var ae *api.Error
	if !errors.As(err, &ae) {
		ae = &api.Error{Code: api.ErrInternal, Message: err.Error()}
	}
	s.logf("WebUI 配额查询失败: [%s] %s", ae.Code, ae.Message)
	_ = api.WriteErr(w, ae)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
