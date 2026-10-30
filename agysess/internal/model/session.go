package model

import (
	"time"
)

// Session 表示一个 agy 会话及其复合状态
type Session struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Preview          string    `json:"preview"`
	StepCount        int       `json:"step_count"`
	LastModifiedTime time.Time `json:"last_modified_time"`
	WorkspaceURIs    string    `json:"workspace_uris"`
	Status           string    `json:"status"`
	ProjectID        string    `json:"project_id"`
	AgentName        string    `json:"agent_name"`

	// 自有扩展元数据（保存在 %APPDATA%/language_projects/agysess/sessions_meta.json）
	IsArchived  bool       `json:"is_archived"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
	CustomNotes string     `json:"custom_notes,omitempty"`
	CustomTitle string     `json:"custom_title,omitempty"`

	// 动态感知运行态（来自 Windows 进程快照）
	IsRunning bool `json:"is_running"`
	PID       int  `json:"pid,omitempty"`
}

// DisplayTitle 获取展示标题（优先自定义标题，其次原生标题，无标题时回退 ID）
func (s *Session) DisplayTitle() string {
	if s.CustomTitle != "" {
		return s.CustomTitle
	}
	if s.Title != "" {
		return s.Title
	}
	if len(s.ID) > 8 {
		return "会话 (" + s.ID[:8] + ")"
	}
	return "会话 (" + s.ID + ")"
}

// FilterOptions 会话列表过滤与投影选项
type FilterOptions struct {
	Query           string `json:"query,omitempty"`
	ProjectID       string `json:"project_id,omitempty"`
	IncludeArchived bool   `json:"include_archived,omitempty"`
	ArchivedOnly    bool   `json:"archived_only,omitempty"`
	SortBy          string `json:"sort_by,omitempty"` // time_desc (default), time_asc, steps_desc, steps_asc, title_asc
	Limit           int    `json:"limit,omitempty"`
	Offset          int    `json:"offset,omitempty"`
}

// CreateSessionOptions 新建会话选项
type CreateSessionOptions struct {
	WorkspaceDir  string `json:"workspace_dir,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	Model         string `json:"model,omitempty"`
	Mode          string `json:"mode,omitempty"`   // accept-edits, auto, plan
	Effort        string `json:"effort,omitempty"` // low, medium, high, xhigh, max
	InitialPrompt string `json:"initial_prompt,omitempty"`
	NewWindow     bool   `json:"new_window,omitempty"`
}

// SessionStats 会话统计概览
type SessionStats struct {
	Total    int `json:"total"`
	Active   int `json:"active"`
	Archived int `json:"archived"`
	Running  int `json:"running"`
}

// ResponseEnvelope CLI 标准 JSON 响应包络
type ResponseEnvelope struct {
	OK    bool           `json:"ok"`
	Data  any            `json:"data,omitempty"`
	Error *ErrorEnvelope `json:"error,omitempty"`
}

// ErrorEnvelope 业务错误包络
type ErrorEnvelope struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}
