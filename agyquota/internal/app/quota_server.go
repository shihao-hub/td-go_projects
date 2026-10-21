package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"time"

	"agyquota/internal/api"
	"agyquota/internal/appdata"
	"agyquota/internal/buildinfo"
	"agyquota/internal/service"
	"agyquota/internal/web"
)

const quotaRequestTimeout = 2 * time.Minute

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

// quotaServer 是 GUI 内嵌的只读 HTTP 层：静态 WebUI + 配额查询。
// 它直接装配 service（与 daemon 共用同一业务核心），不经过 daemon，
// 因此桌面壳无需额外后台进程即可独立工作。
type quotaServer struct {
	svc   *service.Service
	cache *quotaCache
}

func newWebHandler() http.Handler {
	return (&quotaServer{svc: service.New(), cache: newQuotaCache()}).handler()
}

func (s *quotaServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/cache", s.handleCachedQuota)
	mux.HandleFunc("GET /api/quota", s.handleQuota)

	assets, err := fs.Sub(web.Assets, "assets")
	if err != nil {
		logf("加载内嵌 WebUI 失败: %v", err)
	} else {
		mux.Handle("/", http.FileServer(http.FS(assets)))
	}
	return mux
}

func (s *quotaServer) handleMeta(w http.ResponseWriter, _ *http.Request) {
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
func (s *quotaServer) handleQuota(w http.ResponseWriter, r *http.Request) {
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
			writeEnvelopeError(w, err)
			return
		}
		writeOK(w, api.RawResponse{Raw: data})
		return
	}

	snap, err := s.svc.GetQuota(ctx, service.Options{Source: source}, nil)
	if err != nil {
		writeEnvelopeError(w, err)
		return
	}
	s.cache.put(snap)
	logf("WebUI 配额查询完成: source=%s 耗时=%s", source, time.Since(start).Round(time.Millisecond))
	writeOK(w, snap)
}

// handleCachedQuota 返回上次成功查询的缓存快照（没有则 data=null）。
// 桌面壳启动时只读缓存，不发起任何查询。
func (s *quotaServer) handleCachedQuota(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = api.SourceAgy
	}
	snap, ok := s.cache.get(source)
	if !ok {
		writeOK(w, nil)
		return
	}
	writeOK(w, snap)
}

// writeOK 写成功包络并附带 no-store（配额数据时效性强）。
func writeOK(w http.ResponseWriter, data any) {
	w.Header().Set("Cache-Control", "no-store")
	_ = api.WriteOK(w, data)
}

// writeEnvelopeError 把业务错误统一转成失败包络；HTTP 状态保持 200，
// 由前端按 ok 字段分支（与 daemon 契约一致）。
func writeEnvelopeError(w http.ResponseWriter, err error) {
	w.Header().Set("Cache-Control", "no-store")
	var ae *api.Error
	if !errors.As(err, &ae) {
		ae = &api.Error{Code: api.ErrInternal, Message: err.Error()}
	}
	logf("WebUI 配额查询失败: [%s] %s", ae.Code, ae.Message)
	_ = api.WriteErr(w, ae)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// listenEmbedded 绑定回环地址；port=0 时由内核动态探测可用端口。
func listenEmbedded(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

// startEmbeddedServer 在后台启动 WebUI 服务并返回实际访问地址。
func startEmbeddedServer(port int) (string, error) {
	listener, err := listenEmbedded(port)
	if err != nil {
		return "", fmt.Errorf("启动 WebUI 监听失败: %w", err)
	}
	url := "http://" + listener.Addr().String()
	logf("WebUI 已启动: %s", url)
	go func() {
		if err := http.Serve(listener, newWebHandler()); err != nil {
			logf("WebUI 服务退出: %v", err)
		}
	}()
	return url, nil
}

// RunHeadless 是桌面壳不可用（如 WebView2 运行时缺失）时的兜底：
// 只起 WebUI HTTP 服务，可选打开默认浏览器，阻塞至进程退出。
func RunHeadless(openBrowser bool) error {
	listener, err := listenEmbedded(0)
	if err != nil {
		return fmt.Errorf("启动 WebUI 监听失败: %w", err)
	}
	url := "http://" + listener.Addr().String()
	logf("桌面壳不可用，已降级为无窗口 WebUI: %s", url)
	if openBrowser {
		go func() {
			if err := openURL(url); err != nil {
				logf("打开浏览器失败: %v", err)
			}
		}()
	}
	return http.Serve(listener, newWebHandler())
}
