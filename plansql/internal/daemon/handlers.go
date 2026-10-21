package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"plansql/internal/buildinfo"
)

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

// Meta 描述当前服务实例所属的项目根与构建通道，
// 经 /api/v1/meta 暴露给 WebUI，让用户能确认「这个窗口看的是哪个项目」。
type Meta struct {
	RootDir   string `json:"root_dir"`
	SQLFile   string `json:"sql_file"`
	Channel   string `json:"channel"`
	BuildMode string `json:"build_mode"`
	Version   string `json:"version"`
	BuildID   string `json:"build_id"`
	Items     int    `json:"items"`
}

// handleMeta 处理 GET /api/v1/meta
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Error: "method not allowed"})
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	meta := Meta{
		RootDir:   s.rootDir,
		SQLFile:   s.sqlFile,
		Channel:   buildinfo.Channel(),
		BuildMode: buildinfo.BuildMode,
		Version:   buildinfo.Version,
		BuildID:   buildinfo.BuildID,
	}
	if s.store != nil {
		if items, err := s.store.GetAllItems(); err == nil {
			meta.Items = len(items)
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: meta})
}

// RootRequest 切换项目根的请求体
type RootRequest struct {
	Root string `json:"root"`
}

// handleRoot 处理 POST /api/v1/root：切换当前服务观察的项目根。
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Error: "method not allowed"})
		return
	}

	var req RootRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: fmt.Sprintf("invalid request body: %v", err)})
		return
	}
	if req.Root == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: "root 不能为空"})
		return
	}

	sqlFile := filepath.Join(req.Root, "plans_status.sql")
	if err := s.Reconfigure(req.Root, sqlFile); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: err.Error()})
		return
	}

	rootDir, resolvedSQL := s.Project()
	writeJSON(w, http.StatusOK, APIResponse{
		OK: true,
		Data: map[string]any{
			"root_dir": rootDir,
			"sql_file": resolvedSQL,
		},
	})
}

// handleGetItems 处理 GET /api/v1/items
func (s *Server) handleGetItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Error: "method not allowed"})
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	items, err := s.store.GetAllItems()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: items})
}

// CheckRequest 定义校验请求入参
type CheckRequest struct {
	SQLFile string `json:"sql_file,omitempty"`
}

// handleCheck 处理 POST /api/v1/check
func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Error: "method not allowed"})
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	res, err := s.wal.Check()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: res})
}

// handleScan 处理 GET /api/v1/scan
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Error: "method not allowed"})
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	report, err := s.scanner.Scan(s.store)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: report})
}

// MutationRequest 追加变更请求体
type MutationRequest struct {
	Type   string `json:"type"`   // "plan" 或 "spec"
	Path   string `json:"path"`   // 相对路径
	Status string `json:"status"` // 状态 JSON 字符串
}

// handleAppend 处理 POST /api/v1/append
func (s *Server) handleAppend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Error: "method not allowed"})
		return
	}

	var req MutationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	sqlStmt, err := s.wal.AppendMutation(s.store, req.Type, req.Path, req.Status)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		OK: true,
		Data: map[string]any{
			"message": "mutation applied and appended successfully",
			"sql":     sqlStmt,
		},
	})
}
