// Package api 提供 GUI 与 CLI serve 共用的 HTTP 层：
// /api/* 是管理 REST 接口，根路径托管内嵌 WebUI。
//
// 该包刻意不依赖 internal/app（Wails 桌面壳），保证 -tags cli 产物不链接 GUI 依赖。
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"clictl/internal/runner"
	"clictl/internal/service"
	"clictl/internal/web"
)

// Meta 描述服务实例所属通道与数据落点，经 /api/meta 暴露给 WebUI。
type Meta struct {
	Channel   string `json:"channel"`
	BuildMode string `json:"buildMode"`
	Version   string `json:"version"`
	DBPath    string `json:"dbPath"`
}

type Server struct {
	svc  *service.Service
	meta Meta
}

func NewServer(svc *service.Service, meta Meta) *Server {
	return &Server{svc: svc, meta: meta}
}

// Handler 返回 GUI 与 CLI serve 共用的完整 handler（REST + WebUI）。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/preferences", s.handleGetPreferences)
	mux.HandleFunc("POST /api/preferences", s.handlePostPreferences)
	mux.HandleFunc("GET /api/tools", s.handleTools)
	mux.HandleFunc("POST /api/tools", s.handleAdd)
	mux.HandleFunc("GET /api/tools/{name}", s.handleInfo)
	mux.HandleFunc("DELETE /api/tools/{name}", s.handleRemove)
	mux.HandleFunc("PUT /api/tools/{name}/meta", s.handleSetMeta)
	mux.HandleFunc("POST /api/tools/{name}/start", s.handleStart)
	mux.HandleFunc("POST /api/tools/{name}/stop", s.handleStop)
	mux.HandleFunc("POST /api/tools/{name}/reveal", s.handleReveal)
	mux.HandleFunc("GET /api/fs", s.handleFS)

	subFS, err := fs.Sub(web.Assets, "assets")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(subFS)))
	}
	return mux
}

// Listen 在回环地址绑定监听器；port=0 由系统动态探测，dev/release 实例天然不抢端口。
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
}

// URL 由监听器推导实际可访问地址，供 Wails 窗口与 CLI 输出共用。
func URL(listener net.Listener) string {
	return "http://" + listener.Addr().String()
}

// Serve 在已绑定的监听器上阻塞提供服务（宿主需先拿真实端口时用）。
func (s *Server) Serve(listener net.Listener) error {
	return http.Serve(listener, s.Handler())
}

// Start 绑定端口并阻塞提供服务，返回实际访问地址。
func (s *Server) Start(port int, openBrowser bool) (string, error) {
	listener, err := Listen(port)
	if err != nil {
		return "", fmt.Errorf("绑定监听失败: %w", err)
	}
	url := URL(listener)
	fmt.Printf("clictl WebUI is running at %s\n", url)
	if openBrowser {
		go OpenBrowser(url)
	}
	return url, s.Serve(listener)
}

// OpenBrowser 用系统默认浏览器打开地址。
func OpenBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	return exec.Command(cmd, args...).Start()
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	payload := struct {
		Meta
		Tools   int `json:"tools"`
		Running int `json:"running"`
	}{Meta: s.meta}
	if tools, err := s.svc.ListTools(""); err == nil {
		payload.Tools = len(tools)
	}
	if running, err := s.svc.ListRunning(); err == nil {
		payload.Running = len(running)
	}
	writeOK(w, payload)
}

// handleTools 列表：status=active|invalid，running=1 只看后台活实例。
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("running") == "1" || r.URL.Query().Get("running") == "true" {
		res, err := s.svc.ListRunning()
		if err != nil {
			fail(w, err)
			return
		}
		writeOK(w, res)
		return
	}
	res, err := s.svc.ListTools(r.URL.Query().Get("status"))
	if err != nil {
		fail(w, err)
		return
	}
	writeOK(w, res)
}

type addRequest struct {
	Path string          `json:"path"`
	Name string          `json:"name"`
	Desc string          `json:"desc"`
	Meta json.RawMessage `json:"meta"`
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_args", err.Error())
		return
	}
	tool, err := s.svc.Add(req.Path, req.Name, req.Desc, req.Meta)
	if err != nil {
		fail(w, err)
		return
	}
	writeOK(w, tool)
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.Info(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	writeOK(w, res)
}

func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.Remove(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	writeOK(w, res)
}

type setMetaRequest struct {
	Meta json.RawMessage `json:"meta"`
}

func (s *Server) handleSetMeta(w http.ResponseWriter, r *http.Request) {
	var req setMetaRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_args", err.Error())
		return
	}
	if len(strings.TrimSpace(string(req.Meta))) == 0 {
		req.Meta = json.RawMessage("{}")
	}
	tool, err := s.svc.SetMeta(r.PathValue("name"), req.Meta)
	if err != nil {
		fail(w, err)
		return
	}
	writeOK(w, tool)
}

type startRequest struct {
	Args []string `json:"args"`
	Mode string   `json:"mode"` // console | detached；空 = 按 PE 子系统自动选择
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_args", err.Error())
			return
		}
	}
	switch req.Mode {
	case "", runner.ModeConsole, runner.ModeDetached:
	default:
		writeErr(w, http.StatusBadRequest, "bad_args", "mode 仅支持 console | detached")
		return
	}
	res, err := s.svc.StartWithMode(r.PathValue("name"), req.Args, req.Mode)
	if err != nil {
		fail(w, err)
		return
	}
	writeOK(w, res)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.Stop(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	// 与 CLI 语义一致：有杀后仍存活的实例视为部分失败，但结果照常返回。
	writeOK(w, res)
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	info, err := s.svc.Info(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	if err := revealInExplorer(info.Tool.Path); err != nil {
		writeErr(w, http.StatusInternalServerError, "reveal_failed", err.Error())
		return
	}
	writeOK(w, map[string]string{"path": info.Tool.Path})
}

func revealInExplorer(path string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("仅支持 Windows")
	}
	cmd := exec.Command("explorer", "/select,"+path)
	// explorer 成功时也可能返回非 0，忽略退出码，只要进程能拉起。
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// fsEntry 目录浏览条目；WebUI 只能选目录或 .exe 文件。
type fsEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size,omitempty"`
}

type fsResult struct {
	Dir     string    `json:"dir"`
	Parent  string    `json:"parent,omitempty"`
	Drives  []string  `json:"drives,omitempty"`
	Entries []fsEntry `json:"entries"`
}

// handleFS 只读目录浏览，供 WebUI 选择要注册的 exe。
// 不带 dir 时返回盘符列表；只列子目录与 .exe，避免把整个文件系统摊给前端。
func (s *Server) handleFS(w http.ResponseWriter, r *http.Request) {
	dir := strings.TrimSpace(r.URL.Query().Get("dir"))
	if dir == "" {
		writeOK(w, fsResult{Entries: []fsEntry{}, Drives: listDrives()})
		return
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_args", "路径无效: "+err.Error())
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read_dir_failed", "读取目录失败: "+err.Error())
		return
	}

	res := fsResult{Dir: abs, Entries: []fsEntry{}}
	if parent := filepath.Dir(abs); parent != abs {
		res.Parent = parent
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(abs, name)
		if e.IsDir() {
			res.Entries = append(res.Entries, fsEntry{Name: name, Path: path, IsDir: true})
			continue
		}
		if strings.EqualFold(filepath.Ext(name), ".exe") {
			var size int64
			if fi, err := e.Info(); err == nil {
				size = fi.Size()
			}
			res.Entries = append(res.Entries, fsEntry{Name: name, Path: path, Size: size})
		}
	}
	sort.SliceStable(res.Entries, func(i, j int) bool {
		if res.Entries[i].IsDir != res.Entries[j].IsDir {
			return res.Entries[i].IsDir
		}
		return strings.ToLower(res.Entries[i].Name) < strings.ToLower(res.Entries[j].Name)
	})
	writeOK(w, res)
}

func listDrives() []string {
	var drives []string
	for c := 'A'; c <= 'Z'; c++ {
		root := string(c) + ":\\"
		if fi, err := os.Stat(root); err == nil && fi.IsDir() {
			drives = append(drives, root)
		}
	}
	return drives
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("读取请求体失败: %w", err)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("请求体不是合法 JSON: %w", err)
	}
	return nil
}

func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"ok":    false,
		"error": map[string]any{"code": code, "message": message},
	})
}

func fail(w http.ResponseWriter, err error) {
	var se *service.Error
	if !errors.As(err, &se) {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeErr(w, statusFor(se.Code), se.Code, se.Message)
}

// statusFor 业务错误码 → HTTP 状态码（127 等进程退出码语义在 HTTP 层不适用）。
func statusFor(code string) int {
	switch code {
	case "not_found", "file_not_found", "dest_not_found":
		return http.StatusNotFound
	case "conflict", "dest_exists", "already_running", "same_path":
		return http.StatusConflict
	case "bad_args", "not_exe", "meta_invalid", "meta_unknown_key", "meta_too_large":
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
