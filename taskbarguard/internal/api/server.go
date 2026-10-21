package api

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"

	"taskbarguard/internal/core"
	"taskbarguard/internal/web"
)

// Server 把 core.Service 暴露为本地 HTTP 接口，并托管内嵌 WebUI。
// GUI 桌面壳与 CLI `serve` 共用同一个 Handler。
type Server struct {
	service *core.Service
	meta    Meta
}

// Meta 描述当前服务实例的构建通道与数据落点。
type Meta struct {
	Channel       string `json:"channel"`
	Version       string `json:"version"`
	BuildMode     string `json:"buildMode"`
	ConfigPath    string `json:"configPath"`
	ScriptsDir    string `json:"scriptsDir"`
	DataDir       string `json:"dataDir"`
	TaskName      string `json:"taskName"`
	TaskInstalled bool   `json:"taskInstalled"`
	CLIPath       string `json:"cliPath"`
}

// AppView 应用清单与当前偏好合并后的视图。
type AppView struct {
	Name       string   `json:"name"`
	Styles     []string `json:"styles"`
	Script     string   `json:"script"`
	Enabled    bool     `json:"enabled"`
	Style      string   `json:"style,omitempty"`
	CustomIcon string   `json:"custom_icon,omitempty"`
}

func NewServer(service *core.Service, meta Meta) *Server {
	return &Server{service: service, meta: meta}
}

// Handler 返回 GUI 与 CLI serve 共用的完整路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/apps", s.handleApps)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/config", s.handleUpdateConfig)
	mux.HandleFunc("POST /api/apply", s.handleApply)
	mux.HandleFunc("POST /api/revert", s.handleRevert)
	mux.HandleFunc("GET /api/task", s.handleTaskStatus)
	mux.HandleFunc("POST /api/task", s.handleInstallTask)

	subFS, err := fs.Sub(web.Assets, "assets")
	if err != nil {
		panic("内嵌 WebUI 资产缺失: " + err.Error())
	}
	mux.Handle("/", http.FileServer(http.FS(subFS)))
	return mux
}

// Listen 在回环地址绑定监听器；port=0 表示由内核动态探测。
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
}

// URL 由监听器推导实际可访问地址。
func URL(listener net.Listener) string {
	return "http://" + listener.Addr().String()
}

// Serve 在已绑定的监听器上提供服务。
func (s *Server) Serve(listener net.Listener) error {
	return http.Serve(listener, s.Handler())
}

// Start 绑定端口并阻塞服务，返回实际地址。
func (s *Server) Start(port int) (string, error) {
	listener, err := Listen(port)
	if err != nil {
		return "", fmt.Errorf("绑定监听失败: %w", err)
	}
	url := URL(listener)
	return url, s.Serve(listener)
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	meta := s.meta
	meta.TaskInstalled = s.service.TaskInstalled()
	meta.CLIPath = s.service.CLIPath()
	writeData(w, meta)
}

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.service.Apps()
	if err != nil {
		writeError(w, err)
		return
	}
	config, err := s.service.Config()
	if err != nil {
		writeError(w, err)
		return
	}
	views := make([]AppView, 0, len(apps))
	for _, app := range apps {
		preference := config.Apps[app.Name]
		views = append(views, AppView{
			Name:       app.Name,
			Styles:     app.Styles,
			Script:     app.Script,
			Enabled:    preference.Enabled,
			Style:      preference.Style,
			CustomIcon: preference.CustomIcon,
		})
	}
	writeData(w, views)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	config, err := s.service.Config()
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, config)
}

type updateConfigRequest struct {
	App        string `json:"app"`
	Style      string `json:"style"`
	CustomIcon string `json:"custom_icon"`
	Enabled    *bool  `json:"enabled"`
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var request updateConfigRequest
	if err := decodeBody(r, &request); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, failure("invalid_arguments", err.Error()))
		return
	}
	if strings.TrimSpace(request.App) == "" {
		writeJSONStatus(w, http.StatusBadRequest, failure("invalid_arguments", "缺少 app 参数"))
		return
	}

	if request.Style == "" && request.CustomIcon == "" && request.Enabled != nil {
		config, preference, err := s.service.SetEnabled(request.App, *request.Enabled)
		if err != nil {
			writeError(w, err)
			return
		}
		writeData(w, map[string]any{"config": config, "app": preference})
		return
	}

	config, preference, err := s.service.SetPreference(request.App, request.Style, request.CustomIcon)
	if err != nil {
		writeError(w, err)
		return
	}
	writeData(w, map[string]any{"config": config, "app": preference})
}

type applyRequest struct {
	App             string `json:"app"`
	Kill            bool   `json:"kill"`
	RestartExplorer bool   `json:"restart_explorer"`
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	var request applyRequest
	if err := decodeBody(r, &request); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, failure("invalid_arguments", err.Error()))
		return
	}
	applied, output, err := s.service.Apply(core.ApplyOptions{
		App:             request.App,
		Kill:            request.Kill,
		RestartExplorer: request.RestartExplorer,
	})
	if err != nil {
		writeJSONStatus(w, statusForCode(core.ErrorCode(err)), failureWithOutput(core.ErrorCode(err), err.Error(), output))
		return
	}
	writeData(w, map[string]any{"applied": applied, "output": output})
}

type revertRequest struct {
	App             string `json:"app"`
	RestartExplorer bool   `json:"restart_explorer"`
}

func (s *Server) handleRevert(w http.ResponseWriter, r *http.Request) {
	var request revertRequest
	if err := decodeBody(r, &request); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, failure("invalid_arguments", err.Error()))
		return
	}
	if strings.TrimSpace(request.App) == "" {
		writeJSONStatus(w, http.StatusBadRequest, failure("invalid_arguments", "缺少 app 参数"))
		return
	}
	app, output, err := s.service.Revert(request.App, core.RevertOptions{RestartExplorer: request.RestartExplorer})
	if err != nil {
		writeJSONStatus(w, statusForCode(core.ErrorCode(err)), failureWithOutput(core.ErrorCode(err), err.Error(), output))
		return
	}
	writeData(w, map[string]any{"reverted": app.Name, "output": output})
}

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, map[string]any{
		"name":      core.ScheduledTask,
		"installed": s.service.TaskInstalled(),
		"cliPath":   s.service.CLIPath(),
	})
}

func (s *Server) handleInstallTask(w http.ResponseWriter, r *http.Request) {
	output, err := s.service.InstallTask()
	if err != nil {
		writeJSONStatus(w, statusForCode(core.ErrorCode(err)), failureWithOutput(core.ErrorCode(err), err.Error(), output))
		return
	}
	writeData(w, map[string]any{"task": core.ScheduledTask, "output": output})
}

func decodeBody(r *http.Request, target any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	return json.Unmarshal(body, target)
}

type response struct {
	OK    bool           `json:"ok"`
	Data  any            `json:"data,omitempty"`
	Error *responseError `json:"error,omitempty"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Output  string `json:"output,omitempty"`
}

func failure(code, message string) response {
	return response{OK: false, Error: &responseError{Code: code, Message: message}}
}

func failureWithOutput(code, message, output string) response {
	return response{OK: false, Error: &responseError{Code: code, Message: message, Output: output}}
}

func writeData(w http.ResponseWriter, data any) {
	writeJSONStatus(w, http.StatusOK, response{OK: true, Data: data})
}

func writeError(w http.ResponseWriter, err error) {
	writeJSONStatus(w, statusForCode(core.ErrorCode(err)), failure(core.ErrorCode(err), err.Error()))
}

func writeJSONStatus(w http.ResponseWriter, status int, payload response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func statusForCode(code string) int {
	switch code {
	case "invalid_arguments", "app_not_found", "style_not_found", "script_not_found", "app_not_configured":
		return http.StatusBadRequest
	case "config_failed", "script_failed", "task_failed", "executable_failed", "list_failed":
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
