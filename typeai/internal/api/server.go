// Package api 提供 GUI 与 CLI serve 共用的 HTTP 层：REST + SSE + 内嵌 WebUI。
// 本包不依赖 Wails，console 产物可安全链接。
package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"typeai/internal/config"
	"typeai/internal/llm"
	"typeai/internal/service"
	"typeai/internal/session"
	"typeai/internal/web"
)

const (
	maxChatBodyBytes   = 4 << 20
	maxUploadBodyBytes = 20 << 20
)

// Server 持有当前会话与配置，供 GUI 内嵌服务与 CLI serve 共用。
type Server struct {
	mu      sync.Mutex
	chat    *service.Chat
	cfg     config.Config
	dataDir string
	version string
	channel string
	stream  bool
	cancel  context.CancelFunc
}

// Options 构造 Server 所需的依赖。
type Options struct {
	Chat    *service.Chat
	Config  config.Config
	DataDir string
	Version string
	Channel string
}

// NewServer 创建 HTTP 服务实例。
func NewServer(opts Options) *Server {
	return &Server{
		chat:    opts.Chat,
		cfg:     opts.Config,
		dataDir: opts.DataDir,
		version: opts.Version,
		channel: opts.Channel,
	}
}

// Handler 返回完整路由：/api/* 为 REST，其余交给内嵌 WebUI。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/version", s.handleVersion)
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("POST /api/chat/cancel", s.handleCancel)
	mux.HandleFunc("POST /api/branch/fork", s.handleFork)
	mux.HandleFunc("POST /api/branch/switch", s.handleSwitch)
	mux.HandleFunc("POST /api/branch/rename", s.handleRename)
	mux.HandleFunc("POST /api/branch/delete", s.handleDeleteBranch)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("POST /api/sessions/new", s.handleNewSession)
	mux.HandleFunc("POST /api/sessions/resume", s.handleResume)
	mux.HandleFunc("POST /api/sessions/meta", s.handleSessionMeta)
	mux.HandleFunc("POST /api/sessions/delete", s.handleDeleteSession)
	mux.HandleFunc("POST /api/images", s.handleUploadImage)
	mux.HandleFunc("GET /api/image", s.handleImage)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	mux.Handle("/", web.Handler())
	return mux
}

type configView struct {
	BaseURL      string   `json:"base_url"`
	Model        string   `json:"model"`
	APIKeySet    bool     `json:"api_key_set"`
	ConfigPath   string   `json:"config_path"`
	EnvOverrides []string `json:"env_overrides"`
}

type imageView struct {
	Path      string `json:"path"`
	FileName  string `json:"file_name"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
}

type messageView struct {
	Role      string      `json:"role"`
	Content   string      `json:"content"`
	Images    []imageView `json:"images,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

type branchView struct {
	ID               string        `json:"id"`
	ParentID         string        `json:"parent_id"`
	ForkMessageIndex int           `json:"fork_message_index"`
	CreatedAt        time.Time     `json:"created_at"`
	OwnCount         int           `json:"own_count"`
	Messages         []messageView `json:"messages"`
}

type sessionView struct {
	ID             string       `json:"id"`
	Model          string       `json:"model"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	RootBranchID   string       `json:"root_branch_id"`
	ActiveBranchID string       `json:"active_branch_id"`
	Branches       []branchView `json:"branches"`
	Pinned         bool         `json:"pinned"`
	Tags           []string     `json:"tags"`
	DueDate        string       `json:"due_date"`
}

type stateResponse struct {
	Version   string      `json:"version"`
	Channel   string      `json:"channel"`
	Config    configView  `json:"config"`
	Session   sessionView `json:"session"`
	Streaming bool        `json:"streaming"`
}

func (s *Server) snapshot() (*service.Chat, config.Config, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chat, s.cfg, s.stream
}

func (s *Server) toSessionView(sess session.Session) sessionView {
	branches := make([]branchView, 0, len(sess.Branches))
	for _, branch := range sess.Branches {
		resolved, err := session.ResolveMessages(branch.ID, sess.Branches)
		if err != nil {
			resolved = branch.Messages
		}
		branches = append(branches, branchView{
			ID:               branch.ID,
			ParentID:         branch.ParentID,
			ForkMessageIndex: branch.ForkMessageIndex,
			CreatedAt:        branch.CreatedAt,
			OwnCount:         len(branch.Messages),
			Messages:         toMessageViews(resolved),
		})
	}
	tags := sess.Tags
	if tags == nil {
		tags = []string{}
	}
	return sessionView{
		ID:             sess.ID,
		Model:          sess.Model,
		CreatedAt:      sess.CreatedAt,
		UpdatedAt:      sess.UpdatedAt,
		RootBranchID:   sess.RootBranchID,
		ActiveBranchID: sess.ActiveBranchID,
		Branches:       branches,
		Pinned:         sess.Pinned,
		Tags:           tags,
		DueDate:        sess.DueDate,
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	chat, cfg, streaming := s.snapshot()
	writeJSON(w, http.StatusOK, stateResponse{
		Version:   s.version,
		Channel:   s.channel,
		Config:    toConfigView(cfg, s.dataDir),
		Session:   s.toSessionView(chat.Session()),
		Streaming: streaming,
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"name":    "typeai",
		"version": s.version,
		"channel": s.channel,
	})
}

type chatRequest struct {
	BranchID string   `json:"branch_id"`
	Input    string   `json:"input"`
	Images   []string `json:"images"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if !decodeJSON(w, r, maxChatBodyBytes, &req) {
		return
	}

	staged := make([]service.Image, 0, len(req.Images))
	for _, path := range req.Images {
		image, err := service.PrepareImage(path)
		if err != nil {
			writeOperationError(w, err)
			return
		}
		staged = append(staged, image)
	}

	s.mu.Lock()
	if s.stream {
		s.mu.Unlock()
		writeJSONError(w, http.StatusConflict, "busy", "已有回答正在生成，请先等待或取消")
		return
	}
	chat := s.chat
	ctx, cancel := context.WithCancel(r.Context())
	s.stream = true
	s.cancel = cancel
	s.mu.Unlock()

	flusher, ok := w.(http.Flusher)
	if !ok {
		cancel()
		s.finishStream()
		writeJSONError(w, http.StatusInternalServerError, "internal", "当前连接不支持流式输出")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	onDelta := func(delta llm.Delta) {
		writeSSE(w, "delta", map[string]string{
			"kind": string(delta.Kind),
			"text": delta.Text,
		})
		flusher.Flush()
	}

	err := chat.SendBranch(ctx, req.BranchID, req.Input, staged, onDelta)
	cancel()
	s.finishStream()

	if err != nil {
		code, message := operationErrorParts(err)
		writeSSE(w, "error", map[string]string{"code": code, "message": message})
		flusher.Flush()
		return
	}
	writeSSE(w, "done", map[string]any{"session": s.sessionSnapshot()})
	flusher.Flush()
}

func (s *Server) finishStream() {
	s.mu.Lock()
	s.stream = false
	s.cancel = nil
	s.mu.Unlock()
}

func (s *Server) sessionSnapshot() sessionView {
	chat, _, _ := s.snapshot()
	return s.toSessionView(chat.Session())
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": cancel != nil})
}

type forkRequest struct {
	BranchID     string `json:"branch_id"`
	MessageIndex int    `json:"message_index"`
}

func (s *Server) handleFork(w http.ResponseWriter, r *http.Request) {
	var req forkRequest
	if !decodeJSON(w, r, maxChatBodyBytes, &req) {
		return
	}
	chat, _, _ := s.snapshot()
	sess := chat.Session()
	resolved, err := session.ResolveMessages(req.BranchID, sess.Branches)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_args", err.Error())
		return
	}
	if req.MessageIndex < 0 || req.MessageIndex >= len(resolved) || resolved[req.MessageIndex].Role != "assistant" {
		writeJSONError(w, http.StatusBadRequest, "bad_args", "只能在已完成的 AI 消息之后创建分支")
		return
	}
	branch, err := chat.ForkBranch(req.BranchID, req.MessageIndex+1)
	if err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branch": branch, "session": s.sessionSnapshot()})
}

type branchRequest struct {
	BranchID string `json:"branch_id"`
	NewName  string `json:"new_name"`
}

func (s *Server) handleSwitch(w http.ResponseWriter, r *http.Request) {
	var req branchRequest
	if !decodeJSON(w, r, maxChatBodyBytes, &req) {
		return
	}
	chat, _, _ := s.snapshot()
	if err := chat.SetActiveBranch(req.BranchID); err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": s.sessionSnapshot()})
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var req branchRequest
	if !decodeJSON(w, r, maxChatBodyBytes, &req) {
		return
	}
	chat, _, _ := s.snapshot()
	if err := chat.RenameBranch(req.BranchID, req.NewName); err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": s.sessionSnapshot()})
}

func (s *Server) handleDeleteBranch(w http.ResponseWriter, r *http.Request) {
	var req branchRequest
	if !decodeJSON(w, r, maxChatBodyBytes, &req) {
		return
	}
	chat, _, _ := s.snapshot()
	if err := chat.DeleteBranch(req.BranchID); err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": s.sessionSnapshot()})
}

type sessionSummaryView struct {
	ID            string    `json:"id"`
	Model         string    `json:"model"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	FirstUser     string    `json:"first_user"`
	LastAssistant string    `json:"last_assistant"`
	MessageCount  int       `json:"message_count"`
	Branches      []string  `json:"branches"`
	Pinned        bool      `json:"pinned"`
	Tags          []string  `json:"tags"`
	DueDate       string    `json:"due_date"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	chat, _, _ := s.snapshot()
	summaries, err := chat.AllSessionSummaries()
	if err != nil {
		writeOperationError(w, err)
		return
	}
	views := make([]sessionSummaryView, 0, len(summaries))
	for _, summary := range summaries {
		tags := summary.Tags
		if tags == nil {
			tags = []string{}
		}
		views = append(views, sessionSummaryView{
			ID:            summary.ID,
			Model:         summary.Model,
			CreatedAt:     summary.CreatedAt,
			UpdatedAt:     summary.UpdatedAt,
			FirstUser:     summary.FirstUser,
			LastAssistant: summary.LastAssistant,
			MessageCount:  summary.MessageCount,
			Branches:      summary.Branches,
			Pinned:        summary.Pinned,
			Tags:          tags,
			DueDate:       summary.DueDate,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

type sessionMetaRequest struct {
	ID      string    `json:"id"`
	Pinned  *bool     `json:"pinned,omitempty"`
	Tags    *[]string `json:"tags,omitempty"`
	DueDate *string   `json:"due_date,omitempty"`
}

func (s *Server) handleSessionMeta(w http.ResponseWriter, r *http.Request) {
	var req sessionMetaRequest
	if !decodeJSON(w, r, maxChatBodyBytes, &req) {
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_args", "会话 ID 不能为空")
		return
	}
	chat, _, _ := s.snapshot()
	sess, err := chat.UpdateSessionMeta(req.ID, req.Pinned, req.Tags, req.DueDate)
	if err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session": s.toSessionView(*sess),
	})
}

type sessionDeleteRequest struct {
	ID string `json:"id"`
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	var req sessionDeleteRequest
	if !decodeJSON(w, r, maxChatBodyBytes, &req) {
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_args", "会话 ID 不能为空")
		return
	}

	s.mu.Lock()
	if s.stream {
		s.mu.Unlock()
		writeJSONError(w, http.StatusConflict, "busy", "回答生成中，无法删除会话")
		return
	}
	currentID := s.chat.Session().ID
	cfg := s.cfg
	dataDir := s.dataDir
	chat := s.chat
	s.mu.Unlock()

	if err := chat.DeleteSession(req.ID); err != nil {
		writeOperationError(w, err)
		return
	}

	if req.ID == currentID {
		newChat, err := service.NewChat(cfg, dataDir, time.Now())
		if err == nil {
			s.mu.Lock()
			s.chat = newChat
			s.mu.Unlock()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": req.ID, "session": s.sessionSnapshot()})
}

func (s *Server) handleNewSession(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.stream {
		s.mu.Unlock()
		writeJSONError(w, http.StatusConflict, "busy", "回答生成中，无法新建会话")
		return
	}
	cfg := s.cfg
	dataDir := s.dataDir
	s.mu.Unlock()

	chat, err := service.NewChat(cfg, dataDir, time.Now())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	s.mu.Lock()
	s.chat = chat
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"session": s.sessionSnapshot()})
}

type resumeRequest struct {
	ID string `json:"id"`
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	var req resumeRequest
	if !decodeJSON(w, r, maxChatBodyBytes, &req) {
		return
	}
	s.mu.Lock()
	if s.stream {
		s.mu.Unlock()
		writeJSONError(w, http.StatusConflict, "busy", "回答生成中，无法切换会话")
		return
	}
	chat := s.chat
	s.mu.Unlock()

	if _, err := chat.Resume(strings.TrimSpace(req.ID)); err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": s.sessionSnapshot()})
}

type uploadRequest struct {
	FileName   string `json:"file_name"`
	MediaType  string `json:"media_type"`
	DataBase64 string `json:"data_base64"`
}

func (s *Server) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	var req uploadRequest
	if !decodeJSON(w, r, maxUploadBodyBytes, &req) {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.DataBase64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_image", "图片数据不是合法 base64")
		return
	}
	if len(raw) == 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid_image", "图片文件为空")
		return
	}
	if len(raw) > service.MaxImageSize {
		writeJSONError(w, http.StatusBadRequest, "invalid_image", fmt.Sprintf("图片超过 %d MiB 上限", service.MaxImageSize>>20))
		return
	}
	mediaType := http.DetectContentType(raw)
	ext, ok := imageExt(mediaType)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid_image", "仅支持 PNG、JPEG、WebP 或 GIF 图片")
		return
	}

	dir := uploadDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal", "创建图片目录失败")
		return
	}
	name := fmt.Sprintf("%s-%s%s", time.Now().Format("20060102-150405"), randomHex(4), ext)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal", "保存图片失败")
		return
	}

	fileName := sanitizeFileName(req.FileName)
	if fileName == "" {
		fileName = name
	}
	writeJSON(w, http.StatusOK, imageView{
		Path:      path,
		FileName:  fileName,
		MediaType: mediaType,
		Size:      int64(len(raw)),
	})
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "缺少 path 参数", http.StatusBadRequest)
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := imageExtByExt(ext); !ok {
		http.Error(w, "仅支持图片文件", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		http.Error(w, "图片不存在", http.StatusNotFound)
		return
	}
	if info.Size() > service.MaxImageSize {
		http.Error(w, "图片超过大小上限", http.StatusRequestEntityTooLarge)
		return
	}
	http.ServeFile(w, r, path)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	_, cfg, _ := s.snapshot()
	writeJSON(w, http.StatusOK, toConfigView(cfg, s.dataDir))
}

type configRequest struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key"`
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var req configRequest
	if !decodeJSON(w, r, maxChatBodyBytes, &req) {
		return
	}
	disk, err := config.LoadDisk(s.dataDir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	next := config.Config{
		BaseURL: strings.TrimSpace(req.BaseURL),
		Model:   strings.TrimSpace(req.Model),
		APIKey:  strings.TrimSpace(req.APIKey),
	}
	if next.APIKey == "" {
		next.APIKey = disk.APIKey
	}
	if err := config.Save(s.dataDir, next); err != nil {
		var validation *config.ValidationError
		if errors.As(err, &validation) {
			writeJSONError(w, http.StatusBadRequest, "bad_args", validation.Message)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	effective, err := config.Load(s.dataDir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	chat, _, _ := s.snapshot()
	if err := chat.UpdateConfig(effective); err != nil {
		writeOperationError(w, err)
		return
	}
	s.mu.Lock()
	s.cfg = effective
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, toConfigView(effective, s.dataDir))
}

func toConfigView(cfg config.Config, dataDir string) configView {
	return configView{
		BaseURL:      cfg.BaseURL,
		Model:        cfg.Model,
		APIKeySet:    cfg.APIKey != "",
		ConfigPath:   config.Path(dataDir),
		EnvOverrides: envOverrides(),
	}
}

func envOverrides() []string {
	out := make([]string, 0, 3)
	for _, name := range []string{"TYPEAI_BASE_URL", "TYPEAI_API_KEY", "TYPEAI_MODEL"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			out = append(out, name)
		}
	}
	return out
}

func toMessageViews(messages []session.Message) []messageView {
	out := make([]messageView, 0, len(messages))
	for _, message := range messages {
		images := make([]imageView, 0, len(message.Images))
		for _, image := range message.Images {
			images = append(images, imageView{
				Path:      image.Path,
				FileName:  image.FileName,
				MediaType: image.MediaType,
				Size:      image.Size,
			})
		}
		out = append(out, messageView{
			Role:      message.Role,
			Content:   message.Content,
			Images:    images,
			CreatedAt: message.CreatedAt,
		})
	}
	return out
}

func uploadDir() string {
	return filepath.Join(os.TempDir(), "language_projects", "typeai", "uploads")
}

func imageExt(mediaType string) (string, bool) {
	switch mediaType {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/webp":
		return ".webp", true
	case "image/gif":
		return ".gif", true
	default:
		return "", false
	}
}

func imageExtByExt(ext string) (string, bool) {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return ext, true
	default:
		return "", false
	}
}

func sanitizeFileName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			return -1
		case r < 32:
			return -1
		default:
			return r
		}
	}, name)
	return strings.TrimSpace(name)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_args", "请求体不是合法 JSON: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func writeOperationError(w http.ResponseWriter, err error) {
	code, message := operationErrorParts(err)
	status := http.StatusInternalServerError
	switch code {
	case "bad_args", "invalid_image", "not_configured":
		status = http.StatusBadRequest
	}
	writeJSONError(w, status, code, message)
}

func operationErrorParts(err error) (string, string) {
	var operation *service.OperationError
	if errors.As(err, &operation) {
		return operation.ErrorCode(), operation.Error()
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled", "已取消"
	}
	return "internal", err.Error()
}

func writeSSE(w http.ResponseWriter, event string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
}
