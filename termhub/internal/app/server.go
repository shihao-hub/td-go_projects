// Package app 负责装配：本地 HTTP 服务（静态前端 + REST + WS 事件桥）与 Wails 桌面壳。
package app

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"

	"github.com/shihao-hub/termhub/internal/pty"
	"github.com/shihao-hub/termhub/internal/session"
	"github.com/shihao-hub/termhub/internal/web"
)

// APIResponse 统一响应结构。
type APIResponse struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, resp APIResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// Server termhub 的本地服务：前端静态资源 + 接缝方法 + WS 输出桥。
type Server struct {
	svc *session.Service

	mu   sync.Mutex
	addr string
}

// NewServer 依据数据目录构造服务。
func NewServer(dataDir string) *Server {
	_ = dataDir // T06 起落盘会话元数据，T01 预留
	svc := session.NewService(pty.ConPtyFactory{})
	return &Server{svc: svc}
}

// Handler 装配全部路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", web.Handler())
	mux.HandleFunc("GET /api/v1/sessions", s.handleListSessions)
	mux.HandleFunc("POST /api/v1/sessions", s.handleStartSession)
	mux.HandleFunc("GET /api/v1/ws/{id}", s.handleWS)
	return mux
}

// handleListSessions GET /api/v1/sessions
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: s.svc.ListSessions()})
}

// startRequest POST /api/v1/sessions 请求体。
type startRequest struct {
	WorkDir string `json:"work_dir"`
}

// handleStartSession POST /api/v1/sessions
func (s *Server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	opts := pty.Options{WorkDir: req.WorkDir}
	id, err := s.svc.StartSession(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]string{"id": id}})
}

// wsClientMsg WS 上行消息：键盘输入或 resize。
type wsClientMsg struct {
	Type  string `json:"type"` // input / resize
	Data  string `json:"data,omitempty"`
	Cols  int    `json:"cols,omitempty"`
	Rows  int    `json:"rows,omitempty"`
}

// handleWS GET /api/v1/ws/{id}：终端输出下行 + 输入上行的事件桥。
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	backlog, ch, stopped, err := s.svc.Subscribe(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{OK: false, Error: err.Error()})
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	// 回放最近一屏。
	if len(backlog) > 0 {
		if err := conn.Write(r.Context(), websocket.MessageBinary, backlog); err != nil {
			return
		}
	}

	ctx := r.Context()
	// 读泵：上行输入。
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var m wsClientMsg
			if err := json.Unmarshal(data, &m); err != nil {
				// 裸二进制直接当作键盘输入（xterm.js 原生模式兼容）。
				_ = s.svc.WriteInput(id, data)
				continue
			}
			switch m.Type {
			case "input":
				_ = s.svc.WriteInput(id, []byte(m.Data))
			case "resize":
				_ = s.svc.Resize(id, m.Cols, m.Rows)
			}
		}
	}()

	// 写泵 + 会话退出通知。
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.Write(ctx, websocket.MessageBinary, chunk); err != nil {
				return
			}
		case <-stopped:
			// 服务端主动关闭 WS，告知前端会话已结束。
			_ = conn.Close(websocket.StatusNormalClosure, "session exited")
			return
		case <-ctx.Done():
			return
		}
	}
}

// Start 监听本地端口并开始服务，返回 http server（供关闭）。
func (s *Server) Start(port int) (*http.Server, string, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, "", fmt.Errorf("启动 HTTP 监听失败: %w", err)
	}
	s.mu.Lock()
	s.addr = "http://" + listener.Addr().String()
	s.mu.Unlock()

	httpServer := &http.Server{Handler: s.Handler()}
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			_ = s.addr
		}
	}()
	return httpServer, s.URL(), nil
}

// URL 当前服务地址。
func (s *Server) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}
