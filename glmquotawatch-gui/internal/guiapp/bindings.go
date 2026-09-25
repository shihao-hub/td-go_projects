// bindings.go：前端绑定服务（application.Service）。
// 全部方法只做参数转发与结果装配，业务规则在 service 层；
// demo 模式下配置类写操作服务端拒绝（不依赖前端禁用，评审 R-11）。
package guiapp

import (
	"regexp"
	"time"

	"glmquotawatch-gui/internal/service"
)

// 历史范围白名单（小时）；windowKey 形如 TOKENS_LIMIT:3:5。
var (
	historyHours = map[int]bool{24: true, 168: true}
	windowKeyRe  = regexp.MustCompile(`^[A-Z_]+:\d+:\d+$`)
)

// GUIState 前端初始化/刷新的全量状态快照。
type GUIState struct {
	Mode             string              `json:"mode"` // normal | demo
	Status           *service.StatusView `json:"status"`
	LastError        *ErrInfo            `json:"last_error"`
	Config           *service.ConfigView `json:"config"`
	DemoEndsAt       string              `json:"demo_ends_at,omitempty"` // RFC3339
	DemoTotalSeconds int                 `json:"demo_total_seconds"`
	Autostart        bool                `json:"autostart"`
}

// BindingsService 前端可调用方法的宿主（wails3 生成 TS bindings）。
type BindingsService struct {
	rt *MonitorRuntime
}

// GetState 返回全量状态快照。
func (b *BindingsService) GetState() GUIState { return b.rt.Snapshot() }

// SampleNow 立即刷新观察状态；不发送通知，也不推进告警记账。
func (b *BindingsService) SampleNow() (service.StatusView, error) { return b.rt.SampleNow() }

// SetToken 配置 token。
func (b *BindingsService) SetToken(token string) (service.ConfigView, error) {
	svc, err := b.rt.CurrentService()
	if err != nil {
		return service.ConfigView{}, err
	}
	if err := b.requireNormal(); err != nil {
		return service.ConfigView{}, err
	}
	view, err := svc.SetToken(token)
	b.emitConfig(err == nil)
	return view, err
}

// RemoveToken 清除 token（幂等）。
func (b *BindingsService) RemoveToken() (service.ConfigView, error) {
	svc, err := b.rt.CurrentService()
	if err != nil {
		return service.ConfigView{}, err
	}
	if err := b.requireNormal(); err != nil {
		return service.ConfigView{}, err
	}
	view, err := svc.RemoveToken()
	b.emitConfig(err == nil)
	return view, err
}

// SetConfig 修改单个配置键。
func (b *BindingsService) SetConfig(key, value string) (service.ConfigView, error) {
	svc, err := b.rt.CurrentService()
	if err != nil {
		return service.ConfigView{}, err
	}
	if err := b.requireNormal(); err != nil {
		return service.ConfigView{}, err
	}
	view, err := svc.SetConfig(key, value)
	b.emitConfig(err == nil)
	if err == nil && key == "silent" {
		b.rt.ui.SetSilentChecked(view.Silent)
	}
	return view, err
}

// SetSilent 设置静音（设置页开关；托盘走 runtime.ToggleSilent）。
func (b *BindingsService) SetSilent(silent bool) error {
	_, err := b.SetConfig("silent", boolStr(silent))
	return err
}

// ReadHistory 读取指定窗口的历史百分比序列（24h / 7d 白名单）。
func (b *BindingsService) ReadHistory(windowKey string, hours int) ([]service.HistoryPoint, error) {
	svc, err := b.rt.CurrentService()
	if err != nil {
		return nil, err
	}
	if !historyHours[hours] {
		return nil, &service.Error{Code: "bad_args", Message: "hours 仅支持 24（一天）或 168（一周）"}
	}
	if !windowKeyRe.MatchString(windowKey) {
		return nil, &service.Error{Code: "bad_args", Message: "windowKey 形如 TOKENS_LIMIT:3:5"}
	}
	return svc.ReadHistory(windowKey, time.Now().Add(-time.Duration(hours)*time.Hour))
}

// HistoryWindows 返回近 7 天出现过的窗口（历史页选择器）。
func (b *BindingsService) HistoryWindows() ([]service.HistoryWindow, error) {
	svc, err := b.rt.CurrentService()
	if err != nil {
		return nil, err
	}
	return svc.HistoryWindows()
}

// EnterDemo 进入演示模式。
func (b *BindingsService) EnterDemo() error { return b.rt.EnterDemo() }

// ExitDemo 退出演示模式。
func (b *BindingsService) ExitDemo() error { return b.rt.ExitDemo() }

// SetAutostart 设置开机自启（设置/托盘共用；dev 期临时产物拒绝注册）。
func (b *BindingsService) SetAutostart(enabled bool) error {
	return setAutostart(b.rt.app, enabled)
}

// requireNormal demo 模式下拒绝配置类写操作（演示口径只读）。
func (b *BindingsService) requireNormal() error {
	if b.rt.Mode() == ModeDemo {
		return &service.Error{Code: "demo_readonly", Message: "演示模式下配置只读，退出演示后可修改"}
	}
	return nil
}

func (b *BindingsService) emitConfig(ok bool) {
	if !ok {
		return
	}
	b.rt.app.Event.Emit(EventConfig, nil)
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
