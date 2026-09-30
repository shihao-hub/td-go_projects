package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
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

// handleGetItems 处理 GET /api/v1/items
func (s *Server) handleGetItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Error: "method not allowed"})
		return
	}

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
