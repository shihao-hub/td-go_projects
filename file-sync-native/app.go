package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"file-sync-native/config"
	"file-sync-native/engine"
	"file-sync-native/logging"
	"file-sync-native/models"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const emitThrottle = 80 * time.Millisecond

// App 是绑定给前端的方法集，替代旧版的 REST API 层。
type App struct {
	ctx   context.Context
	cfg   *config.Config
	cache *engine.HashCache

	mu   sync.Mutex
	runs map[string]*syncRun

	lastMu sync.Mutex
	last   map[string]lastProgress
}

type lastProgress struct {
	p  models.SyncProgress
	at time.Time
}

func (a *App) setLast(id string, p models.SyncProgress) {
	a.lastMu.Lock()
	defer a.lastMu.Unlock()
	if a.last == nil {
		a.last = make(map[string]lastProgress)
	}
	a.last[id] = lastProgress{p: p, at: time.Now()}
}

func (a *App) getLast(id string) (models.SyncProgress, bool) {
	a.lastMu.Lock()
	defer a.lastMu.Unlock()
	lp, ok := a.last[id]
	if !ok || time.Since(lp.at) > time.Minute {
		return models.SyncProgress{}, false
	}
	switch lp.p.Status {
	case models.StatusCompleted, models.StatusError, models.StatusCancelled:
		return lp.p, true
	}
	return models.SyncProgress{}, false
}

// syncRun 记录一个进行中的同步：进度、取消、删除确认通道。
type syncRun struct {
	tracker *engine.Tracker
	cancel  context.CancelFunc
	confirm chan bool
}

func (r *syncRun) awaitConfirm(ctx context.Context) bool {
	select {
	case ok := <-r.confirm:
		return ok
	case <-ctx.Done():
		return false
	}
}

func NewApp(cfg *config.Config, cache *engine.HashCache) *App {
	return &App{cfg: cfg, cache: cache, runs: make(map[string]*syncRun)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// ---------- 任务 CRUD ----------

type TaskInput struct {
	Name        string   `json:"name"`
	SourcePath  string   `json:"source_path"`
	TargetPath  string   `json:"target_path"`
	IgnoreRules []string `json:"ignore_rules"`
}

func pathToName(p string) string {
	p = strings.TrimRight(strings.TrimSpace(p), `/\`)
	p = strings.ReplaceAll(p, `\`, "/")
	p = strings.ReplaceAll(p, ":", "")
	p = strings.ReplaceAll(p, "/", "-")
	return strings.Trim(p, "-")
}

func normalizeTaskInput(req *TaskInput) error {
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

func (a *App) checkTargetConflict(taskID, target string) error {
	for _, t := range a.cfg.ListTasks() {
		if t.ID != taskID && strings.EqualFold(filepath.Clean(t.TargetPath), filepath.Clean(target)) {
			return fmt.Errorf("目标目录已被任务「%s」占用（%s），同一目标目录只允许一个同步任务，请换一个目录", t.Name, t.TargetPath)
		}
	}
	return nil
}

func (a *App) ListTasks() []*models.SyncTask {
	return a.cfg.ListTasks()
}

func (a *App) CreateTask(req TaskInput) (*models.SyncTask, error) {
	if err := normalizeTaskInput(&req); err != nil {
		return nil, err
	}
	task := models.NewSyncTask(req.Name, req.SourcePath, req.TargetPath, req.IgnoreRules)
	if err := a.checkTargetConflict(task.ID, task.TargetPath); err != nil {
		return nil, err
	}
	if err := a.cfg.AddTask(task); err != nil {
		return nil, err
	}
	logging.Infof("config", "新增任务: id=%s name=%q %s -> %s 规则数=%d", task.ID, task.Name, task.SourcePath, task.TargetPath, len(task.IgnoreRules))
	return task, nil
}

func (a *App) UpdateTask(id string, req TaskInput) (*models.SyncTask, error) {
	task, err := a.cfg.GetTask(id)
	if err != nil {
		return nil, err
	}
	if a.isRunning(id) {
		return nil, errors.New("任务正在同步，无法编辑")
	}
	if err := normalizeTaskInput(&req); err != nil {
		return nil, err
	}
	task.Name = req.Name
	task.SourcePath = req.SourcePath
	task.TargetPath = req.TargetPath
	task.IgnoreRules = req.IgnoreRules
	if err := a.checkTargetConflict(task.ID, task.TargetPath); err != nil {
		return nil, err
	}
	if err := a.cfg.UpdateTask(task); err != nil {
		return nil, err
	}
	logging.Infof("config", "更新任务: id=%s name=%q %s -> %s 规则数=%d", task.ID, task.Name, task.SourcePath, task.TargetPath, len(task.IgnoreRules))
	return task, nil
}

func (a *App) DeleteTask(id string) error {
	if a.isRunning(id) {
		return errors.New("任务正在同步，无法删除")
	}
	task, err := a.cfg.GetTask(id)
	if err != nil {
		return err
	}
	if err := a.cfg.DeleteTask(id); err != nil {
		return err
	}
	logging.Infof("config", "删除任务: id=%s name=%q", id, task.Name)
	return nil
}

// ---------- 设置 ----------

type Settings struct {
	BackupRoot string `json:"backup_root"`
}

func (a *App) GetSettings() Settings {
	return Settings{BackupRoot: a.cfg.GetBackupRoot()}
}

func (a *App) UpdateSettings(backupRoot string) (Settings, error) {
	root := strings.TrimSpace(backupRoot)
	if root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return Settings{}, errors.New("目录路径无效: " + err.Error())
		}
		root = filepath.Clean(abs)
	}
	if err := a.cfg.SetBackupRoot(root); err != nil {
		return Settings{}, errors.New("保存设置失败: " + err.Error())
	}
	logging.Infof("config", "更新设置: backup_root=%q", root)
	return Settings{BackupRoot: root}, nil
}

// ---------- 目录选择 ----------

// PickFolder 打开系统原生目录选择框，取消返回空串。
func (a *App) PickFolder(defaultPath string) (string, error) {
	opts := runtime.OpenDialogOptions{Title: "选择目录"}
	if defaultPath != "" {
		if fi, err := os.Stat(defaultPath); err == nil && fi.IsDir() {
			opts.DefaultDirectory = defaultPath
		}
	}
	return runtime.OpenDirectoryDialog(a.ctx, opts)
}

// ---------- 扫描与同步 ----------

func (a *App) isRunning(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.runs[id]
	return ok
}

// ScanTask 干跑模式算差异，不落盘。
func (a *App) ScanTask(id string) (*models.FileDiff, error) {
	task, err := a.cfg.GetTask(id)
	if err != nil {
		return nil, err
	}
	if a.isRunning(id) {
		return nil, errors.New("任务正在同步")
	}
	res, err := engine.Run(context.Background(), task, a.cache, engine.NewTracker(id), engine.Options{DryRun: true})
	if err != nil {
		return nil, err
	}
	return &res.Diff, nil
}

// StartSync 启动后台同步；进度通过 "sync:progress" 事件推送，
// 结束汇总通过 "sync:finished" 推送。
func (a *App) StartSync(id string, force bool) error {
	task, err := a.cfg.GetTask(id)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(task.SourcePath); err != nil || !fi.IsDir() {
		return errors.New("源目录不存在或不是目录")
	}
	if fi, err := os.Stat(task.TargetPath); err == nil && !fi.IsDir() {
		return errors.New("目标路径已存在且不是目录")
	}

	a.mu.Lock()
	if _, running := a.runs[id]; running {
		a.mu.Unlock()
		return errors.New("任务正在同步")
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &syncRun{tracker: engine.NewTracker(id), cancel: cancel, confirm: make(chan bool, 1)}
	a.runs[id] = run
	a.mu.Unlock()

	tr := run.tracker
	tr.SetNotify(a.throttledEmit())
	log := logging.With("sync")
	log.Info("任务开始", "task", task.Name, "id", task.ID, "source", task.SourcePath, "target", task.TargetPath, "force", force)

	go func() {
		started := time.Now()
		res, runErr := engine.Run(ctx, task, a.cache, tr, engine.Options{
			Force: force,
			OnPendingDeletes: func([]string) bool {
				return run.awaitConfirm(ctx)
			},
		})
		p := tr.Snapshot()
		log.Info("任务结束", "task", task.Name, "id", task.ID, "status", p.Status,
			"copied", res.Copied, "skipped", res.Skipped, "deleted", res.Deleted,
			"elapsed", time.Since(started).Round(time.Millisecond), "error", firstErr(runErr, res.Errors))
		if runErr == nil {
			task.LastSync = time.Now()
			_ = a.cfg.Save()
		}
		a.setLast(task.ID, p)
		a.mu.Lock()
		delete(a.runs, id)
		a.mu.Unlock()
		fin := map[string]any{"task_id": id, "status": p.Status, "copied": res.Copied,
			"skipped": res.Skipped, "deleted": res.Deleted, "declined": res.DeletedDeclined,
			"errors": res.Errors}
		if runErr != nil {
			fin["error"] = runErr.Error()
		}
		a.emitEvent("sync:finished", fin)
	}()
	return nil
}

func firstErr(err error, errs []string) string {
	if err != nil {
		return err.Error()
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return ""
}

// emitEvent 包装事件发射：脱离 Wails 环境（单测）时静默跳过。
func (a *App) emitEvent(name string, data ...any) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, name, data...)
}

// throttledEmit 包装进度事件：80ms 节流，终态（完成/出错/取消/等待删除确认）必发。
func (a *App) throttledEmit() func(models.SyncProgress) {
	var last time.Time
	return func(p models.SyncProgress) {
		now := time.Now()
		terminal := p.Status == models.StatusCompleted || p.Status == models.StatusError ||
			p.Status == models.StatusCancelled || p.Status == models.StatusAwaitingDelete
		if !terminal && now.Sub(last) < emitThrottle {
			return
		}
		last = now
		a.emitEvent("sync:progress", p)
	}
}

// Progress 返回任务当前进度快照；无运行时回退到最近一次终态（1 分钟内）。
func (a *App) Progress(id string) models.SyncProgress {
	a.mu.Lock()
	run, ok := a.runs[id]
	a.mu.Unlock()
	if !ok {
		if lp, ok := a.getLast(id); ok {
			return lp
		}
		return models.SyncProgress{TaskID: id, Status: "idle"}
	}
	return run.tracker.Snapshot()
}

func (a *App) CancelSync(id string) error {
	a.mu.Lock()
	run, ok := a.runs[id]
	a.mu.Unlock()
	if !ok {
		return errors.New("没有正在进行的同步")
	}
	run.cancel()
	return nil
}

// ConfirmDeletes 响应删除二次确认；ok=false 表示保留这些文件。
func (a *App) ConfirmDeletes(id string, ok bool) error {
	a.mu.Lock()
	run, running := a.runs[id]
	a.mu.Unlock()
	if !running {
		return errors.New("没有等待确认的同步")
	}
	select {
	case run.confirm <- ok:
		return nil
	default:
		return errors.New("该任务不在删除确认阶段")
	}
}
