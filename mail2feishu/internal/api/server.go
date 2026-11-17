// Package api 把 runtime 暴露为本地 HTTP 接口，并托管内嵌 WebUI。
package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mail2feishu/internal/buildinfo"
	"mail2feishu/internal/runtime"
	"mail2feishu/internal/web"
)

const (
	testWebhookTimeout = 30 * time.Second
	testMailTimeout    = 30 * time.Second

	// tokenHeader 是 WebUI 与外部脚本携带访问令牌的请求头。
	tokenHeader = "X-Auth-Token"
)

// Server 是 GUI 与 CLI 共用的 HTTP 服务。
type Server struct {
	runtime *runtime.Runtime
	token   string
}

// NewServer 创建 API 服务，并生成随机会话令牌。
// 令牌只注入 WebUI 窗口地址，未携带令牌的 /api 请求一律拒绝，
// 防止本机其他进程或已发现端口的网页调用发信、重连与配置接口。
func NewServer(rt *runtime.Runtime) *Server {
	return &Server{runtime: rt, token: newToken()}
}

// Token 返回会话令牌，供 GUI 拼接带令牌的窗口地址。
func (s *Server) Token() string { return s.token }

func newToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 不可用的极端情况下仍要求携带令牌，只是强度下降。
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

// Handler 返回完整路由（API + 静态资源）。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("PUT /api/config", s.handleUpdateConfig)
	mux.HandleFunc("GET /api/records", s.handleRecords)
	mux.HandleFunc("GET /api/logs", s.handleLogs)
	mux.HandleFunc("POST /api/test-webhook", s.handleTestWebhook)
	mux.HandleFunc("POST /api/test-mail", s.handleTestMail)
	mux.HandleFunc("POST /api/reconnect", s.handleReconnect)

	subFS, err := fs.Sub(web.Assets, "assets")
	if err != nil {
		panic("内嵌 WebUI 资产缺失: " + err.Error())
	}
	mux.Handle("/", http.FileServer(http.FS(subFS)))
	return s.guard(mux)
}

// guard 校验来源与访问令牌：只接受回环地址访问，拒绝跨站 Origin，
// 且 /api/ 下的所有请求都必须携带会话令牌。
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			writeFailure(w, http.StatusForbidden, "forbidden_host", "只允许从本机回环地址访问")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
			writeFailure(w, http.StatusForbidden, "forbidden_origin", "拒绝跨站请求")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && !s.authorized(r) {
			writeFailure(w, http.StatusUnauthorized, "unauthorized", "缺少或错误的访问令牌")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(r *http.Request) bool {
	token := r.Header.Get(tokenHeader)
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if parsed, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sameOrigin(origin, hostport string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, hostport)
}

// Listen 在回环地址绑定监听器；port=0 表示由内核动态分配。
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
}

// URL 由监听器推导实际地址。
func URL(listener net.Listener) string {
	return "http://" + listener.Addr().String()
}

// Serve 在已绑定的监听器上提供服务。
func (s *Server) Serve(listener net.Listener) error {
	return http.Serve(listener, s.Handler())
}

type response struct {
	OK    bool           `json:"ok"`
	Data  any            `json:"data,omitempty"`
	Error *responseError `json:"error,omitempty"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeData(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, response{OK: true, Data: data})
}

func writeFailure(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, response{OK: false, Error: &responseError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, payload response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleMeta(w http.ResponseWriter, _ *http.Request) {
	writeData(w, map[string]any{
		"version":     buildinfo.Version,
		"mode":        buildinfo.Mode,
		"build_id":    buildinfo.BuildID,
		"config_path": s.runtime.ConfigPath(),
		"data_dir":    s.runtime.DataDir(),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeData(w, s.runtime.Status())
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeData(w, s.runtime.Config())
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var view runtime.ConfigView
	if err := decodeBody(r, &view); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid_arguments", err.Error())
		return
	}
	if err := s.runtime.UpdateConfig(view); err != nil {
		writeFailure(w, http.StatusBadRequest, "config_invalid", err.Error())
		return
	}
	writeData(w, map[string]any{
		"config": s.runtime.Config(),
		"status": s.runtime.Status(),
	})
}

func (s *Server) handleRecords(w http.ResponseWriter, r *http.Request) {
	limit := intQuery(r, "limit", 200, 1000)
	events, err := s.runtime.Records(limit)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "records_failed", err.Error())
		return
	}
	writeData(w, events)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	lines := intQuery(r, "tail", 200, 1000)
	content, err := s.runtime.LogTail(lines)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "logs_failed", err.Error())
		return
	}
	writeData(w, map[string]any{"lines": content})
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), testWebhookTimeout)
	defer cancel()
	if err := s.runtime.TestWebhook(ctx); err != nil {
		writeFailure(w, http.StatusBadGateway, "webhook_failed", err.Error())
		return
	}
	writeData(w, map[string]any{"sent": true})
}

func (s *Server) handleTestMail(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), testMailTimeout)
	defer cancel()
	if err := s.runtime.SendTestMail(ctx); err != nil {
		if errors.Is(err, runtime.ErrTestSenderMissing) {
			writeFailure(w, http.StatusBadRequest, "test_sender_missing", err.Error())
			return
		}
		writeFailure(w, http.StatusBadGateway, "test_mail_failed", err.Error())
		return
	}
	writeData(w, map[string]any{
		"sent": true,
		"to":   s.runtime.Config().IMAP.Username,
	})
}

func (s *Server) handleReconnect(w http.ResponseWriter, _ *http.Request) {
	if err := s.runtime.Reconnect(); err != nil {
		writeFailure(w, http.StatusInternalServerError, "reconnect_failed", err.Error())
		return
	}
	writeData(w, s.runtime.Status())
}

func decodeBody(r *http.Request, target any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("请求体为空")
	}
	return json.Unmarshal(body, target)
}

func intQuery(r *http.Request, key string, fallback, max int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}
