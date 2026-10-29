package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nutvault/internal/core"
	"nutvault/internal/storage"
	"nutvault/internal/web"
)

type Meta struct {
	Channel         string                             `json:"channel"`
	BuildMode       string                             `json:"build_mode"`
	Version         string                             `json:"version"`
	DBPath          string                             `json:"db_path"`
	IsAutoStart     func() bool                        `json:"-"`
	SetAutoStart    func(bool) error                   `json:"-"`
	SelectFile      func(title string) (string, error) `json:"-"`
	SelectDirectory func(title string) (string, error) `json:"-"`
}

type Server struct {
	store     *storage.Store
	engine    *core.Engine
	scheduler *core.Scheduler
	meta      Meta
	mux       *http.ServeMux
}

func NewServer(store *storage.Store, meta Meta) *Server {
	engine := core.NewEngine(store)
	scheduler := core.NewScheduler(engine, store)
	scheduler.Start()

	s := &Server{
		store:     store,
		engine:    engine,
		scheduler: scheduler,
		meta:      meta,
		mux:       http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Engine() *core.Engine {
	return s.engine
}

func (s *Server) Scheduler() *core.Scheduler {
	return s.scheduler
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func Listen(port int) (net.Listener, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	return net.Listen("tcp", addr)
}

func URL(l net.Listener) string {
	return fmt.Sprintf("http://%s", l.Addr().String())
}

func (s *Server) Serve(l net.Listener) error {
	return http.Serve(l, s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/meta", s.handleMeta)
	s.mux.HandleFunc("/api/stats", s.handleStats)
	s.mux.HandleFunc("/api/targets", s.handleTargets)
	s.mux.HandleFunc("/api/targets/", s.handleSingleTargetRoutes)
	s.mux.HandleFunc("/api/backup-all", s.handleBackupAll)
	s.mux.HandleFunc("/api/snapshots/", s.handleSnapshotRoutes)
	s.mux.HandleFunc("/api/config", s.handleConfig)
	s.mux.HandleFunc("/api/preview-path", s.handlePreviewPath)
	s.mux.HandleFunc("/api/open-folder", s.handleOpenFolder)
	s.mux.HandleFunc("/api/select-file", s.handleSelectFile)
	s.mux.HandleFunc("/api/select-directory", s.handleSelectDirectory)

	distHandler := web.DistHandler()
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		distHandler.ServeHTTP(w, r)
	})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.meta)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	targets, _ := s.store.ListTargets()
	totalSnaps := 0
	for _, t := range targets {
		snaps, _ := s.store.ListSnapshotsForTarget(t.ID, 1000)
		totalSnaps += len(snaps)
	}

	backupBytes, restoreBytes, _ := s.store.GetMonthTrafficUsage()
	schedStatus := s.scheduler.GetStatus()

	writeJSON(w, http.StatusOK, map[string]any{
		"total_targets":       len(targets),
		"total_snapshots":     totalSnaps,
		"month_backup_bytes":  backupBytes,
		"month_restore_bytes": restoreBytes,
		"scheduler":           schedStatus,
		"nutstore_root":       s.engine.GetNutstoreRoot(),
	})
}

func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		targets, err := s.store.ListTargets()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, targets)
	case http.MethodPost:
		var req struct {
			SourcePath      string `json:"source_path"`
			MaxSizeLimit    int64  `json:"max_size_limit"`
			KeepVersions    int    `json:"keep_versions"`
			ScheduleEnabled bool   `json:"schedule_enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "解析 JSON 失败")
			return
		}
		if strings.TrimSpace(req.SourcePath) == "" {
			writeError(w, http.StatusBadRequest, "源文件路径不可为空")
			return
		}

		info := core.AnalyzePath(req.SourcePath)
		if _, err := os.Stat(info.SourcePath); err != nil {
			writeError(w, http.StatusBadRequest, "指定本地文件不存在: "+info.SourcePath)
			return
		}

		if existing, _ := s.store.GetTargetBySourcePath(info.SourcePath); existing != nil {
			writeError(w, http.StatusConflict, "该文件保护规则已存在")
			return
		}

		target := &storage.Target{
			SourcePath:      info.SourcePath,
			SourceDir:       info.SourceDir,
			FileName:        info.FileName,
			MappedDir:       info.MappedDir,
			MaxSizeLimit:    req.MaxSizeLimit,
			KeepVersions:    req.KeepVersions,
			ScheduleEnabled: req.ScheduleEnabled,
		}
		saved, err := s.store.AddTarget(target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存目标失败: "+err.Error())
			return
		}

		_, _ = s.engine.BackupTarget(saved, false)

		writeJSON(w, http.StatusCreated, saved)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSingleTargetRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/targets/")
	parts := strings.Split(path, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效 target ID")
		return
	}

	target, err := s.store.GetTargetByID(id)
	if err != nil || target == nil {
		writeError(w, http.StatusNotFound, "未找到目标规则")
		return
	}

	if len(parts) == 1 {
		if r.Method == http.MethodDelete {
			if err := s.store.DeleteTarget(id); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		writeJSON(w, http.StatusOK, target)
		return
	}

	action := parts[1]
	switch action {
	case "backup":
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		res, err := s.engine.BackupTarget(target, true)
		if err != nil && res == nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, res)
	case "snapshots":
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		snaps, err := s.store.ListSnapshotsForTarget(id, 200)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, snaps)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleBackupAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	results := s.scheduler.TriggerAll()
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleSnapshotRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/snapshots/")
	parts := strings.Split(path, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效快照 ID")
		return
	}

	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	action := parts[1]
	switch action {
	case "diff":
		diff, err := s.engine.CompareSnapshotWithSource(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, diff)
	case "restore":
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		res, err := s.engine.RestoreSnapshot(id, "", true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, res)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfgs, err := s.store.GetAllConfigs()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		maxSize, _ := strconv.ParseInt(cfgs["max_file_size_bytes"], 10, 64)
		if maxSize <= 0 {
			maxSize = core.DefaultMaxFileSizeBytes
		}
		schedInterval, _ := strconv.Atoi(cfgs["global_schedule_interval_mins"])
		if schedInterval <= 0 {
			schedInterval = core.DefaultScheduleInterval
		}
		root := cfgs["nutstore_root"]
		if root == "" {
			root = core.DefaultNutstoreRoot
		}

		autoStartEnabled := false
		if s.meta.IsAutoStart != nil {
			autoStartEnabled = s.meta.IsAutoStart()
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"nutstore_root":                 root,
			"max_file_size_bytes":           maxSize,
			"global_schedule_interval_mins": schedInterval,
			"autostart":                     autoStartEnabled,
		})
	case http.MethodPost:
		var req struct {
			NutstoreRoot         string `json:"nutstore_root"`
			MaxFileSizeBytes     int64  `json:"max_file_size_bytes"`
			ScheduleIntervalMins int    `json:"global_schedule_interval_mins"`
			AutoStart            bool   `json:"autostart"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "解析请求体失败")
			return
		}

		if req.NutstoreRoot != "" {
			_ = s.store.SetConfig("nutstore_root", req.NutstoreRoot)
		}
		if req.MaxFileSizeBytes > 0 {
			_ = s.store.SetConfig("max_file_size_bytes", strconv.FormatInt(req.MaxFileSizeBytes, 10))
		}
		if req.ScheduleIntervalMins > 0 {
			_ = s.store.SetConfig("global_schedule_interval_mins", strconv.Itoa(req.ScheduleIntervalMins))
		}
		if s.meta.SetAutoStart != nil {
			_ = s.meta.SetAutoStart(req.AutoStart)
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePreviewPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		writeError(w, http.StatusBadRequest, "缺少 path 参数")
		return
	}

	info := core.AnalyzePath(req.Path)
	example := core.FormatSnapshotFilename(info.FileName, time.Now())

	writeJSON(w, http.StatusOK, map[string]string{
		"source_path":      info.SourcePath,
		"source_dir":       info.SourceDir,
		"file_name":        info.FileName,
		"mapped_dir":       info.MappedDir,
		"snapshot_example": example,
	})
}

func (s *Server) handleOpenFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Type      string `json:"type"` // nutstore_root, mapped_dir, local_file
		MappedDir string `json:"mapped_dir"`
		Path      string `json:"path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	var targetDir string
	switch req.Type {
	case "nutstore_root":
		targetDir = s.engine.GetNutstoreRoot()
	case "mapped_dir":
		targetDir = filepath.Join(s.engine.GetNutstoreRoot(), req.MappedDir)
	case "local_file":
		targetDir = filepath.Dir(req.Path)
	}

	if targetDir != "" {
		_ = os.MkdirAll(targetDir, 0o755)
		_ = exec.Command("explorer", targetDir).Start()
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSelectFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	title := req.Title
	if title == "" {
		title = "选择要保护的文件"
	}

	var selected string
	var err error
	if s.meta.SelectFile != nil {
		selected, err = s.meta.SelectFile(title)
	} else {
		selected, err = fallbackSelectFile(title)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": selected})
}

func (s *Server) handleSelectDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	title := req.Title
	if title == "" {
		title = "选择坚果云同步目录"
	}

	var selected string
	var err error
	if s.meta.SelectDirectory != nil {
		selected, err = s.meta.SelectDirectory(title)
	} else {
		selected, err = fallbackSelectDirectory(title)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": selected})
}

func fallbackSelectFile(title string) (string, error) {
	cmdScript := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.OpenFileDialog; $f.Title = '%s'; $f.Filter = '所有文件 (*.*)|*.*'; [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $f.FileName }`, strings.ReplaceAll(title, "'", "''"))
	out, err := exec.Command("powershell", "-NoProfile", "-Command", cmdScript).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func fallbackSelectDirectory(title string) (string, error) {
	cmdScript := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.FolderBrowserDialog; $f.Description = '%s'; $f.ShowNewFolderButton = $true; [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $f.SelectedPath }`, strings.ReplaceAll(title, "'", "''"))
	out, err := exec.Command("powershell", "-NoProfile", "-Command", cmdScript).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
