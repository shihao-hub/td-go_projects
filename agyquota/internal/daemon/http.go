package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"agyquota/internal/api"
	"agyquota/internal/service"
)

// handler 承载 daemon 的 HTTP 层：路由、包络、SSE、握手校验与活动计数。
type handler struct {
	svc     *service.Service
	version string
	buildID string
	pid     int
	started time.Time

	// inflight/lastActivity 供空闲监控读取：进入 +1、退出 -1 并刷新 lastActivity。
	inflight     atomic.Int64
	lastActivity atomic.Int64 // UnixNano
	shuttingDown atomic.Bool

	// triggerShutdown 由 serve 注入：POST /v1/stop 请求优雅退出。
	triggerShutdown func(reason string)
}

// Handler 返回带活动计数与关闭检查的完整路由。
func (h *handler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+api.EndpointPing, h.handlePing)
	mux.HandleFunc("POST "+api.EndpointQuotaGet, h.handleQuotaGet)
	mux.HandleFunc("POST "+api.EndpointQuotaRaw, h.handleQuotaRaw)
	mux.HandleFunc("POST "+api.EndpointStop, h.handleStop)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.inflight.Add(1)
		defer func() {
			h.inflight.Add(-1)
			h.lastActivity.Store(time.Now().UnixNano())
		}()
		if h.shuttingDown.Load() {
			// 新请求 503 + daemon_unreachable：客户端按不可达处理，生产可重新拉起。
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = api.WriteErr(w, &api.Error{Code: api.ErrDaemonUnreachable, Message: "agyquota daemon 正在退出"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (h *handler) handlePing(w http.ResponseWriter, _ *http.Request) {
	_ = api.WriteOK(w, api.PingResponse{
		Name:      "agyquota",
		Version:   h.version,
		BuildID:   h.buildID,
		PID:       h.pid,
		StartedAt: h.started,
	})
}

func (h *handler) handleQuotaGet(w http.ResponseWriter, r *http.Request) {
	if !h.checkBuild(w, r) {
		return
	}
	req, ok := decodeQuotaRequest(w, r)
	if !ok {
		return
	}
	h.serveQuota(w, r, func(ctx context.Context, progress func(string)) (any, error) {
		return h.svc.GetQuota(ctx, optionsFrom(req), progress)
	})
}

func (h *handler) handleQuotaRaw(w http.ResponseWriter, r *http.Request) {
	if !h.checkBuild(w, r) {
		return
	}
	req, ok := decodeQuotaRequest(w, r)
	if !ok {
		return
	}
	h.serveQuota(w, r, func(ctx context.Context, progress func(string)) (any, error) {
		raw, err := h.svc.FetchRaw(ctx, optionsFrom(req), progress)
		if err != nil {
			return nil, err
		}
		return api.RawResponse{Raw: raw}, nil
	})
}

// handleStop 先写成功包络并 Flush，再由 goroutine 触发 shutdown，
// 保证响应先落地、不受根 ctx cancel 影响。
func (h *handler) handleStop(w http.ResponseWriter, _ *http.Request) {
	_ = api.WriteOK(w, api.StopResponse{Stopped: true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	if h.triggerShutdown != nil {
		go h.triggerShutdown("收到 stop 请求")
	}
}

// checkBuild 校验 /v1/quota/* 的 buildID 头；不通过时写 200 + build_mismatch 包络。
func (h *handler) checkBuild(w http.ResponseWriter, r *http.Request) bool {
	got := r.Header.Get(api.HeaderBuild)
	if got == h.buildID {
		return true
	}
	client := got
	if client == "" {
		client = "未提供"
	}
	_ = api.WriteErr(w, &api.Error{
		Code:    api.ErrBuildMismatch,
		Message: fmt.Sprintf("buildID 不一致：客户端 %s / 服务端 %s，请运行 agyquota stop 后重试", client, h.buildID),
	})
	return false
}

// decodeQuotaRequest 解码请求体；语法错误 → 400 + bad_args，空体按零值请求处理。
func decodeQuotaRequest(w http.ResponseWriter, r *http.Request) (api.QuotaRequest, bool) {
	var req api.QuotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		w.WriteHeader(http.StatusBadRequest)
		_ = api.WriteErr(w, &api.Error{Code: api.ErrBadArgs, Message: fmt.Sprintf("请求体不是合法 JSON: %v", err)})
		return req, false
	}
	return req, true
}

func optionsFrom(req api.QuotaRequest) service.Options {
	return service.Options{Source: req.Source, TokenFile: req.TokenFile}
}

// serveQuota 执行一次业务调用：Accept 含 text/event-stream 时走 SSE
// （progress 帧 + result 帧），否则整体包络返回。
func (h *handler) serveQuota(w http.ResponseWriter, r *http.Request, call func(ctx context.Context, progress func(string)) (any, error)) {
	flusher, canFlush := w.(http.Flusher)
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") || !canFlush {
		data, err := call(r.Context(), nil)
		_ = api.WriteEnvelope(w, envelopeFor(data, err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// progress 回调在 handler goroutine 内同步直写（agy 日志回调为同步调用）。
	progress := func(msg string) {
		b, _ := json.Marshal(map[string]string{"message": msg})
		fmt.Fprintf(w, "event: progress\ndata: %s\n\n", b)
		flusher.Flush()
	}
	data, err := call(r.Context(), progress)
	b, mErr := api.MarshalEnvelope(envelopeFor(data, err))
	if mErr != nil {
		b, _ = api.MarshalEnvelope(api.Envelope{OK: false, Error: &api.Error{Code: api.ErrInternal, Message: mErr.Error()}})
	}
	fmt.Fprintf(w, "event: result\ndata: %s\n\n", b)
	flusher.Flush()
}

// envelopeFor 把业务结果/错误统一转成包络：*api.Error 原样透传，其他 error 归 internal。
func envelopeFor(data any, err error) api.Envelope {
	if err != nil {
		var ae *api.Error
		if !errors.As(err, &ae) {
			ae = &api.Error{Code: api.ErrInternal, Message: err.Error()}
		}
		return api.Envelope{OK: false, Error: ae}
	}
	b, mErr := json.Marshal(data)
	if mErr != nil {
		return api.Envelope{OK: false, Error: &api.Error{Code: api.ErrInternal, Message: mErr.Error()}}
	}
	return api.Envelope{OK: true, Data: b}
}
