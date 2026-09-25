// runtime.go：监控模式运行时——normal/demo 双态管理、daemon 生命周期、
// 事件推送（status / sample-error / mode-changed / config-changed）、
// 托盘联动（经 uiBridge 单向更新）与通知适配。
package guiapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"glmquotawatch-gui/internal/api"
	"glmquotawatch-gui/internal/service"
	"glmquotawatch-gui/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// 运行模式常量。
const (
	ModeNormal = "normal"
	ModeDemo   = "demo"
)

// 事件名常量（前端 Events.On 使用同一组名字）。
const (
	EventStatus    = "status"
	EventSampleErr = "sample-error"
	EventNotifyErr = "notify-error"
	EventMode      = "mode-changed"
	EventConfig    = "config-changed"
)

// uiBridge runtime → GUI 外观的单向更新接口（App 实现；测试可注入 fake）。
type uiBridge interface {
	SetTooltip(text string)
	SetSilentChecked(checked bool)
	SetAutostartChecked(checked bool)
	SetDemoMode(demo bool)
	NotifyError(title, message string)
}

// toastNotifier Wails 通知适配器：实现 service.Notifier。
// silent=true 仍发通知但无声（与归档版 Audio=Silent 语义一致）。
type toastNotifier struct {
	ns *notifications.NotificationService
}

func (t toastNotifier) Notify(title, msg string, silent bool) error {
	opts := notifications.NotificationOptions{
		ID:    fmt.Sprintf("glmquotawatch-gui-%d", time.Now().UnixNano()),
		Title: title,
		Body:  msg,
	}
	if silent {
		opts.Sound = &notifications.NotificationSound{Silent: true}
	}
	return t.ns.SendNotification(opts)
}

// MonitorRuntime 模式运行时：持有当前模式的 service 与 daemon 生命周期。
type MonitorRuntime struct {
	app *application.App
	ns  *notifications.NotificationService
	ui  uiBridge

	mu         sync.Mutex
	mode       string
	svc        *service.Service
	cancel     context.CancelFunc
	done       chan struct{}
	lastStatus *service.StatusView
	lastErr    *ErrInfo
	demoEndsAt time.Time
}

// ErrInfo 前端错误态结构。
type ErrInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// newMonitorRuntime 构造运行时（Start 之前不触碰任何数据目录）。
func newMonitorRuntime(app *application.App, ns *notifications.NotificationService) *MonitorRuntime {
	return &MonitorRuntime{
		app:  app,
		ns:   ns,
		mode: ModeNormal,
	}
}

// SetUI 注入外观桥（App 构建托盘后调用；仅启动阶段写一次）。
func (r *MonitorRuntime) SetUI(ui uiBridge) { r.ui = ui }

func (r *MonitorRuntime) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil)) // GUI 期日志静默（错误走事件与托盘）
}

// Start 启动监控：demo=true 直接进入演示模式，否则正常模式。
func (r *MonitorRuntime) Start(demo bool) {
	if demo {
		if err := r.EnterDemo(); err != nil && r.ui != nil {
			r.ui.NotifyError("进入演示模式失败", err.Error())
		}
		return
	}
	r.startNormal()
}

// Stop 停止当前 daemon（应用退出前调用）。
func (r *MonitorRuntime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopDaemonLocked()
}

// Mode 返回当前模式（并发安全）。
func (r *MonitorRuntime) Mode() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mode
}

// startNormal 构造真实模式：默认数据目录 + 真实上游。
func (r *MonitorRuntime) startNormal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mode == ModeNormal && r.svc != nil {
		return // 已在运行
	}
	r.stopDaemonLocked()
	base, err := store.DefaultDir()
	if err != nil {
		if r.ui != nil {
			r.ui.NotifyError("定位数据目录失败", err.Error())
		}
		return
	}
	svc, err := service.Open(base) // 真实 fetcher（token 每轮现读）+ 环境变量 baseURL
	if err != nil {
		if r.ui != nil {
			r.ui.NotifyError("打开数据目录失败", err.Error())
		}
		return
	}
	r.mode = ModeNormal
	r.svc = svc
	r.lastErr = nil
	r.startDaemonLocked()
	if r.ui != nil {
		r.ui.SetDemoMode(false)
	}
	// 同步托盘静音勾选与 tooltip 基线
	if cfg, cerr := svc.GetConfig(); cerr == nil && r.ui != nil {
		r.ui.SetSilentChecked(cfg.Silent)
	}
	r.app.Event.Emit(EventMode, map[string]string{"mode": r.mode})
}

// EnterDemo 进入演示模式（FR-11）：停真实 daemon → 清 demo 目录（失败拒绝）
// → 写演示配置 + 虚拟 token → DemoFetcher + 固定 5s 间隔 → 起 daemon。
// 全程不触碰真实数据目录（AC-11）。
func (r *MonitorRuntime) EnterDemo() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mode == ModeDemo {
		return nil
	}
	r.stopDaemonLocked()

	base, err := store.DefaultDir()
	if err != nil {
		return err
	}
	demoDir := filepath.Join(base, "demo")
	// runtime 层一级防线：目标必须恰为 默认目录/demo（全等，禁止前缀匹配）
	if filepath.Base(demoDir) != "demo" || filepath.Clean(demoDir) == filepath.Clean(base) {
		return errors.New("internal: 演示目录推导异常，拒绝进入演示")
	}
	wipe, err := store.Open(base) // 任一 Store 实例可执行（ResetDir 内含二级防线）
	if err != nil {
		return err
	}
	if err := wipe.ResetDir(demoDir); err != nil {
		return fmt.Errorf("演示目录清场失败，已拒绝进入演示: %w", err)
	}

	demoStore, err := store.Open(demoDir)
	if err != nil {
		return err
	}
	// 演示初始配置：虚拟 token（64 字符）+ 默认档位；间隔经 WithFixedInterval 固定 5s
	if err := demoStore.SaveConfig(store.Config{
		Token:      "demo-virtual-token-0123456789abcdef-0123456789abcdef",
		Interval:   "5s",
		Thresholds: []int{50, 60, 80, 90},
		Hysteresis: 5,
	}); err != nil {
		return err
	}
	fetcher := api.NewDemoFetcher()
	r.mode = ModeDemo
	r.svc = service.New(demoStore,
		service.WithFetcherFactory(func() api.Fetcher { return fetcher }),
		service.WithFixedInterval(5*time.Second))
	r.lastErr = nil
	r.demoEndsAt = time.Now().Add(api.DemoRealDuration)
	r.startDaemonLocked()
	if r.ui != nil {
		r.ui.SetDemoMode(true)
		r.ui.SetTooltip("GLM 用量监控 · 演示模式")
	}
	r.app.Event.Emit(EventMode, map[string]any{
		"mode":               r.mode,
		"demo_ends_at":       r.demoEndsAt.Format(time.RFC3339),
		"demo_total_seconds": int(api.DemoRealDuration.Seconds()),
	})
	return nil
}

// ExitDemo 退出演示：重建真实模式 service（真实目录原状态继续，AC-11 后半）。
func (r *MonitorRuntime) ExitDemo() error {
	r.startNormal() // startNormal 内部处理停 daemon 与重建
	return nil
}

// ---- daemon 生命周期 ----

func (r *MonitorRuntime) startDaemonLocked() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	done := make(chan struct{})
	r.done = done
	svc := r.svc
	notifier := toastNotifier{ns: r.ns}
	log := r.logger()
	go func() {
		defer close(done)
		_ = svc.RunDaemon(ctx, notifier, log, service.DaemonHooks{
			OnSample:      r.onSample,
			OnError:       r.onError,
			OnNotifyError: r.onNotifyError,
		})
	}()
}

// stopDaemonLocked 取消 daemon 并等待其退出（EnterDemo/Stop 持锁调用）。
func (r *MonitorRuntime) stopDaemonLocked() {
	if r.cancel != nil {
		r.cancel()
		<-r.done // RunDaemon 对 ctx 取消即时返回，等待有界
		r.cancel = nil
		r.done = nil
	}
}

// ---- 采样回调（daemon goroutine 调用）----

func (r *MonitorRuntime) onSample(view service.StatusView) {
	r.mu.Lock()
	v := view
	r.lastStatus = &v
	r.lastErr = nil
	r.mu.Unlock()
	r.app.Event.Emit(EventStatus, v)
	if r.ui != nil {
		r.ui.SetTooltip(r.tooltipText())
	}
}

func (r *MonitorRuntime) onError(err error) {
	r.mu.Lock()
	e := ErrInfo{Code: "internal", Message: err.Error()}
	var se *service.Error
	if errors.As(err, &se) {
		e = ErrInfo{Code: se.Code, Message: se.Message}
	}
	r.lastErr = &e
	r.mu.Unlock()
	r.app.Event.Emit(EventSampleErr, e)
	if r.ui != nil {
		r.ui.SetTooltip(r.tooltipText())
	}
}

func (r *MonitorRuntime) onNotifyError(err error) {
	r.mu.Lock()
	e := ErrInfo{Code: "internal", Message: err.Error()}
	var se *service.Error
	if errors.As(err, &se) {
		e = ErrInfo{Code: se.Code, Message: se.Message}
	}
	r.lastErr = &e
	r.mu.Unlock()
	r.app.Event.Emit(EventNotifyErr, e)
	if r.ui != nil {
		r.ui.SetTooltip(r.tooltipText())
	}
}

// tooltipText 三态托盘文案：正常 / 已告警 / 采样失败（FR-6、评审 R-14）。
func (r *MonitorRuntime) tooltipText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mode == ModeDemo {
		base := "GLM 用量监控 · 演示模式"
		if r.lastStatus != nil && len(r.lastStatus.Windows) > 0 {
			w := r.lastStatus.Windows[0]
			base = fmt.Sprintf("%s · %d%%", base, w.Percentage)
		}
		return base
	}
	if r.lastErr != nil {
		switch r.lastErr.Code {
		case "notify_failed":
			return "GLM 用量监控（告警发送失败，下轮重试）"
		case "state_save_failed":
			return "GLM 用量监控（告警状态保存失败）"
		default:
			return "GLM 用量监控（采样失败，下轮自动重试）"
		}
	}
	if r.lastStatus == nil || len(r.lastStatus.Windows) == 0 {
		return "GLM 用量监控"
	}
	var top *service.WindowView
	var highestNotified int
	for i := range r.lastStatus.Windows {
		w := &r.lastStatus.Windows[i]
		if top == nil || w.Percentage > top.Percentage {
			top = w
		}
		if n := len(w.Notified); n > 0 && w.Notified[n-1] > highestNotified {
			highestNotified = w.Notified[n-1]
		}
	}
	text := fmt.Sprintf("GLM 用量监控 · 最高 %d%%（%s）", top.Percentage, top.Label)
	if highestNotified > 0 {
		text += fmt.Sprintf(" · 已告警 %d%%", highestNotified)
	}
	return text
}

// ---- 前端绑定与托盘共用的操作 ----

// SampleNow 手动立即采样；只刷新观察视图，不发送通知或推进告警记账。
func (r *MonitorRuntime) SampleNow() (service.StatusView, error) {
	r.mu.Lock()
	svc := r.svc
	r.mu.Unlock()
	if svc == nil {
		return service.StatusView{}, errors.New("internal: 监控未启动")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := svc.SampleUncommitted(ctx)
	if err != nil {
		return view, err
	}
	r.onSample(view)
	return view, nil
}

// ToggleSilent 切换静音（托盘菜单用）；demo 模式拒绝写。
func (r *MonitorRuntime) ToggleSilent() error {
	r.mu.Lock()
	svc, mode := r.svc, r.mode
	r.mu.Unlock()
	if svc == nil {
		return errors.New("internal: 监控未启动")
	}
	if mode == ModeDemo {
		return &service.Error{Code: "demo_readonly", Message: "演示模式下配置只读"}
	}
	cfg, err := svc.GetConfig()
	if err != nil {
		return err
	}
	view, err := svc.SetConfig("silent", fmt.Sprintf("%t", !cfg.Silent))
	if err != nil {
		return err
	}
	if r.ui != nil {
		r.ui.SetSilentChecked(view.Silent)
	}
	r.app.Event.Emit(EventConfig, view)
	return nil
}

// ToggleAutostart 切换开机自启（托盘菜单用）。
func (r *MonitorRuntime) ToggleAutostart() error {
	enabled := r.AutostartEnabled()
	err := setAutostart(r.app, !enabled)
	if err != nil {
		return err
	}
	if r.ui != nil {
		r.ui.SetAutostartChecked(!enabled)
	}
	return nil
}

// AutostartEnabled 查询自启注册状态。
func (r *MonitorRuntime) AutostartEnabled() bool {
	return autostartEnabled(r.app)
}

// CurrentService 返回当前模式 service（bindings 只读写经此）。
func (r *MonitorRuntime) CurrentService() (*service.Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.svc == nil {
		return nil, errors.New("internal: 监控未启动")
	}
	return r.svc, nil
}

// Snapshot 汇总 GUI 状态（GetState 用）。
func (r *MonitorRuntime) Snapshot() GUIState {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := GUIState{
		Mode:             r.mode,
		DemoTotalSeconds: int(api.DemoRealDuration.Seconds()),
		Autostart:        autostartEnabled(r.app),
	}
	if r.lastStatus != nil {
		v := *r.lastStatus
		st.Status = &v
	}
	if r.lastErr != nil {
		e := *r.lastErr
		st.LastError = &e
	}
	if r.svc != nil {
		if cfg, err := r.svc.GetConfig(); err == nil {
			st.Config = &cfg
		}
	}
	if r.mode == ModeDemo {
		st.DemoEndsAt = r.demoEndsAt.Format(time.RFC3339)
	}
	return st
}
