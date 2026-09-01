package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	stdsync "sync"
	"time"

	"file-sync/config"
	fsync "file-sync/sync"
	"file-sync/ignore"
	"file-sync/logging"
	"file-sync/models"
)

type Server struct {
	cfg    *config.Config
	runs   stdsync.Map
	lastMu stdsync.Mutex
	last   map[string]lastProgress
	http   *http.Server
}

type lastProgress struct {
	p  models.SyncProgress
	at time.Time
}

func (s *Server) setLast(id string, p models.SyncProgress) {
	s.lastMu.Lock()
	defer s.lastMu.Unlock()
	if s.last == nil {
		s.last = make(map[string]lastProgress)
	}
	s.last[id] = lastProgress{p: p, at: time.Now()}
}

func (s *Server) getLast(id string) (models.SyncProgress, bool) {
	s.lastMu.Lock()
	defer s.lastMu.Unlock()
	lp, ok := s.last[id]
	if !ok || time.Since(lp.at) > time.Minute {
		return models.SyncProgress{}, false
	}
	if lp.p.Status != "completed" && lp.p.Status != "error" {
		return models.SyncProgress{}, false
	}
	return lp.p, true
}

type syncRun struct {
	tracker *fsync.ProgressTracker
	cancel  context.CancelFunc
}

type taskRequest struct {
	Name        string   `json:"name"`
	SourcePath  string   `json:"source_path"`
	TargetPath  string   `json:"target_path"`
	IgnoreRules []string `json:"ignore_rules"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	logging.Errorf("api", "HTTP %d: %s", status, msg)
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeBody(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	return json.Unmarshal(body, v)
}

func pathToName(p string) string {
	p = strings.TrimRight(strings.TrimSpace(p), `/\`)
	p = strings.ReplaceAll(p, `\`, "/")
	p = strings.ReplaceAll(p, ":", "")
	p = strings.ReplaceAll(p, "/", "-")
	return strings.Trim(p, "-")
}

func normalizeTaskRequest(req *taskRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.SourcePath = strings.TrimSpace(req.SourcePath)
	req.TargetPath = strings.TrimSpace(req.TargetPath)
	if req.SourcePath == "" || req.TargetPath == "" {
		return errors.New("源目录和目标目录不能为空")
	}
	src, err := filepath.Abs(req.SourcePath)
	if err != nil {
		return errors.New("源路径无效: " + err.Error())
	}
	dst, err := filepath.Abs(req.TargetPath)
	if err != nil {
		return errors.New("目标路径无效: " + err.Error())
	}
	req.SourcePath = filepath.Clean(src)
	req.TargetPath = filepath.Clean(dst)
	if req.Name == "" {
		req.Name = pathToName(req.SourcePath)
	}
	if req.Name == "" {
		return errors.New("任务名称不能为空")
	}
	return checkPaths(req.SourcePath, req.TargetPath)
}

func checkPaths(src, dst string) error {
	if strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) {
		return errors.New("源目录和目标目录不能相同")
	}
	if within(src, dst) {
		return errors.New("目标目录不能位于源目录内部")
	}
	if within(dst, src) {
		return errors.New("源目录不能位于目标目录内部")
	}
	return nil
}

func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Server) checkTargetConflict(taskID, target string) error {
	for _, t := range s.cfg.ListTasks() {
		if t.ID != taskID && strings.EqualFold(filepath.Clean(t.TargetPath), filepath.Clean(target)) {
			return fmt.Errorf("目标目录已被任务「%s」占用（%s），同一目标目录只允许一个同步任务，请换一个目录", t.Name, t.TargetPath)
		}
	}
	return nil
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"backup_root": s.cfg.GetBackupRoot()})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BackupRoot string `json:"backup_root"`
	}
	if err := decodeBody(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	root := strings.TrimSpace(req.BackupRoot)
	if root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "目录路径无效: "+err.Error())
			return
		}
		root = filepath.Clean(abs)
	}
	if err := s.cfg.SetBackupRoot(root); err != nil {
		jsonError(w, http.StatusInternalServerError, "保存设置失败: "+err.Error())
		return
	}
	logging.Infof("config", "更新设置: backup_root=%q", root)
	writeJSON(w, http.StatusOK, map[string]string{"backup_root": root})
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.ListTasks())
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req taskRequest
	if err := decodeBody(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	if err := normalizeTaskRequest(&req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	task := models.NewSyncTask(req.Name, req.SourcePath, req.TargetPath, req.IgnoreRules)
	if err := s.checkTargetConflict(task.ID, task.TargetPath); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.cfg.AddTask(task); err != nil {
		jsonError(w, http.StatusInternalServerError, "保存配置失败: "+err.Error())
		return
	}
	logging.Infof("config", "新增任务: id=%s name=%q %s -> %s 规则数=%d", task.ID, task.Name, task.SourcePath, task.TargetPath, len(task.IgnoreRules))
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.cfg.GetTask(r.PathValue("id"))
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.cfg.GetTask(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	if _, running := s.runs.Load(id); running {
		jsonError(w, http.StatusConflict, "任务正在同步，无法编辑")
		return
	}
	var req taskRequest
	if err := decodeBody(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	if err := normalizeTaskRequest(&req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	task.Name = req.Name
	task.SourcePath = req.SourcePath
	task.TargetPath = req.TargetPath
	task.IgnoreRules = req.IgnoreRules
	if err := s.checkTargetConflict(task.ID, task.TargetPath); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.cfg.UpdateTask(task); err != nil {
		jsonError(w, http.StatusInternalServerError, "保存配置失败: "+err.Error())
		return
	}
	logging.Infof("config", "更新任务: id=%s name=%q %s -> %s 规则数=%d", task.ID, task.Name, task.SourcePath, task.TargetPath, len(task.IgnoreRules))
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, running := s.runs.Load(id); running {
		jsonError(w, http.StatusConflict, "任务正在同步，无法删除")
		return
	}
	task, err := s.cfg.GetTask(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.cfg.DeleteTask(id); err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	logging.Infof("config", "删除任务: id=%s name=%q", id, task.Name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) scanTrees(task *models.SyncTask) (*models.FileTree, *models.FileTree, error) {
	if fi, err := os.Stat(task.SourcePath); err != nil || !fi.IsDir() {
		return nil, nil, errors.New("源目录不存在或不是目录")
	}
	m := ignore.NewMatcher(task.IgnoreRules)
	srcTree, err := fsync.ScanDirectory(task.SourcePath, m, nil)
	if err != nil {
		return nil, nil, errors.New("扫描源目录失败: " + err.Error())
	}
	dstTree := &models.FileTree{Files: map[string]*models.FileInfo{}}
	if fi, err := os.Stat(task.TargetPath); err == nil {
		if !fi.IsDir() {
			return nil, nil, errors.New("目标路径已存在且不是目录")
		}
		dstTree, err = fsync.ScanDirectory(task.TargetPath, m, nil)
		if err != nil {
			return nil, nil, errors.New("扫描目标目录失败: " + err.Error())
		}
	}
	return srcTree, dstTree, nil
}

func (s *Server) handleScanTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.cfg.GetTask(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	if _, running := s.runs.Load(id); running {
		jsonError(w, http.StatusConflict, "任务正在同步")
		return
	}
	srcTree, dstTree, err := s.scanTrees(task)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fsync.ComputeDiff(srcTree, dstTree))
}

func (s *Server) handleSyncTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.cfg.GetTask(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	if _, running := s.runs.Load(id); running {
		jsonError(w, http.StatusConflict, "任务正在同步")
		return
	}
	var req struct {
		Force bool `json:"force"`
	}
	q := strings.ToLower(r.URL.Query().Get("force"))
	if q == "1" || q == "true" || q == "yes" {
		req.Force = true
	}
	if err := decodeBody(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	if fi, err := os.Stat(task.SourcePath); err != nil || !fi.IsDir() {
		jsonError(w, http.StatusBadRequest, "源目录不存在或不是目录")
		return
	}
	if fi, err := os.Stat(task.TargetPath); err == nil && !fi.IsDir() {
		jsonError(w, http.StatusBadRequest, "目标路径已存在且不是目录")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	run := &syncRun{tracker: fsync.NewProgressTracker(id), cancel: cancel}
	if _, loaded := s.runs.LoadOrStore(id, run); loaded {
		cancel()
		jsonError(w, http.StatusConflict, "任务正在同步")
		return
	}
	go s.runSync(ctx, task, run, req.Force)
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "started", "task_id": id, "force": req.Force})
}

func (s *Server) runSync(ctx context.Context, task *models.SyncTask, run *syncRun, force bool) {
	t := run.tracker
	started := time.Now()
	log := logging.With("sync")
	log.Info("任务开始", "task", task.Name, "id", task.ID, "source", task.SourcePath, "target", task.TargetPath, "force", force)
	defer func() {
		// sh-note: t.p 是状态，这个状态在特定时刻更新，这里只需要知道是收尾，这个状态是否准确由其他人负责
		p := t.Snapshot()
		log.Info("任务结束", "task", task.Name, "id", task.ID, "status", p.Status, "elapsed", time.Since(started).Round(time.Millisecond), "error", p.ErrorMessage)
		s.setLast(task.ID, p)
		s.runs.Delete(task.ID)
	}()
	m := ignore.NewMatcher(task.IgnoreRules)

	t.SetStatus("scanning")
	n := 0
	cb := func(rel string) { n++; t.SetScanProgress(n, rel) }

	scanStart := time.Now()
	srcTree, err := fsync.ScanDirectory(task.SourcePath, m, cb)
	if err != nil {
		t.Fail("扫描源目录失败: " + err.Error())
		log.Error("扫描源目录失败", "task", task.Name, "err", err)
		return
	}
	log.Info("源目录扫描完成", "files", len(srcTree.Files), "errors", len(srcTree.Errors), "elapsed", time.Since(scanStart).Round(time.Millisecond))

	dstTree := &models.FileTree{Files: map[string]*models.FileInfo{}}
	if _, statErr := os.Stat(task.TargetPath); statErr == nil {
		scanStart = time.Now()
		dstTree, err = fsync.ScanDirectory(task.TargetPath, m, cb)
		if err != nil {
			t.Fail("扫描目标目录失败: " + err.Error())
			log.Error("扫描目标目录失败", "task", task.Name, "err", err)
			return
		}
		log.Info("目标目录扫描完成", "files", len(dstTree.Files), "errors", len(dstTree.Errors), "elapsed", time.Since(scanStart).Round(time.Millisecond))
	} else if err := os.MkdirAll(task.TargetPath, 0o755); err != nil {
		t.Fail("创建目标目录失败: " + err.Error())
		log.Error("创建目标目录失败", "task", task.Name, "err", err)
		return
	}

	diff := fsync.ComputeDiff(srcTree, dstTree)
	if force {
		diff.Modified = nil
		diff.Added = fsync.AllFiles(srcTree)
	}
	log.Info("差异汇总", "added", len(diff.Added), "modified", len(diff.Modified), "deleted", len(diff.Deleted))

	if diff.Total() == 0 {
		t.Complete()
	} else if errs := fsync.Execute(ctx, task.SourcePath, task.TargetPath, diff, t); len(errs) > 0 {
		for _, e := range errs {
			log.Error("文件操作失败", "task", task.Name, "err", e)
		}
		return
	}

	if ctx.Err() != nil {
		return
	}
	task.LastSync = time.Now()
	_ = s.cfg.Save()
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	v, ok := s.runs.Load(id)
	if !ok {
		jsonError(w, http.StatusNotFound, "没有正在进行的同步")
		return
	}
	v.(*syncRun).cancel()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	send := func(v any) {
		data, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}
	ping := func() {
		fmt.Fprint(w, ": ping\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}

	send(models.SyncProgress{TaskID: id, Status: "idle"})

	if lp, ok := s.getLast(id); ok {
		send(lp)
		return
	}

	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()

	var run *syncRun
	for run == nil {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			ping()
		case <-poll.C:
			if v, ok := s.runs.Load(id); ok {
				run = v.(*syncRun)
			} else if lp, ok := s.getLast(id); ok {
				send(lp)
				return
			}
		}
	}

	ch, unsub := run.tracker.Subscribe()
	defer unsub()
	send(run.tracker.Snapshot())

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			ping()
		case p, ok := <-ch:
			if !ok {
				return
			}
			send(p)
			if p.Status == "completed" || p.Status == "error" {
				return
			}
		}
	}
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	resp := struct {
		Path   string  `json:"path"`
		Parent string  `json:"parent,omitempty"`
		Dirs   []entry `json:"dirs"`
	}{Dirs: []entry{}}

	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		if home, err := os.UserHomeDir(); err == nil {
			p = home
		} else if wd, err := os.Getwd(); err == nil {
			p = wd
		}
	}
	p = filepath.Clean(p)

	fi, err := os.Stat(p)
	if err != nil {
		for c := 'A'; c <= 'Z'; c++ {
			root := string(c) + `:\`
			if _, err := os.Stat(root); err == nil {
				resp.Dirs = append(resp.Dirs, entry{Name: root, Path: root})
			}
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if !fi.IsDir() {
		p = filepath.Dir(p)
	}

	entries, err := os.ReadDir(p)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "无法读取目录: "+err.Error())
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			resp.Dirs = append(resp.Dirs, entry{Name: e.Name(), Path: filepath.Join(p, e.Name())})
		}
	}
	resp.Path = p
	if parent := filepath.Dir(p); parent != p {
		resp.Parent = parent
	}
	writeJSON(w, http.StatusOK, resp)
}
