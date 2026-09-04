// Package ui 组装 aiquick 主界面。UI 层保持"薄"：
// 所有数据动作经 client 调 aiquickd，本包不做业务逻辑。
package ui

import (
	"context"
	"encoding/json"
	"errors"
	"image/color"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"aiquick/internal/api"
	"aiquick/internal/client"
	"aiquick/internal/hotkey"
	"aiquick/internal/protocol"
)

// 预置常量
const (
	prefLastPreset  = "lastPresetID"
	prefHotkey      = "hotkey"
	prefCapture     = "captureEnabled"
	defaultHotkeyS  = "Alt+S"
	captureMaxRunes = 5000

	askTimeout    = 120 * time.Second
	shortTimeout  = 5 * time.Second
	heartInterval = 10 * time.Second
	heartTimeout  = 3 * time.Second
)

var (
	colorGreen = color.NRGBA{R: 0x2e, G: 0xcc, B: 0x71, A: 0xff}
	colorRed   = color.NRGBA{R: 0xe7, G: 0x4c, B: 0x3c, A: 0xff}
	colorGray  = color.NRGBA{R: 0x95, A: 0x5f, B: 0x8f, G: 0xa5}
)

// App 主界面状态与组件。
type App struct {
	fa   fyne.App
	win  fyne.Window
	cli  *client.Client
	logs *LogBuffer
	hk   *hotkey.Manager

	// 数据（仅 fyne 主协程触碰）
	presets   []api.Preset
	nameToID  map[string]string
	outputTxt string

	// 并发字段
	curRid     atomic.Int64 // 当前流式请求 rid（0 = 无）
	sending    atomic.Bool
	askCancel  context.CancelFunc
	curCombo   hotkey.Combo
	armedCombo bool // 设置页热键捕获中

	// 组件
	presetSelect *widget.Select
	input        *widget.Entry
	sendBtn      *widget.Button
	output       *widget.Label
	outputScroll *container.Scroll
	copyBtn      *widget.Button
	statusDot    *canvas.Text
	statusText   *widget.Label
	modelName    string
}

// New 构建 App（不显示窗口）。
func New(fa fyne.App, cli *client.Client, logs *LogBuffer, hk *hotkey.Manager) *App {
	a := &App{fa: fa, cli: cli, logs: logs, hk: hk, nameToID: map[string]string{}}
	a.win = fa.NewWindow("aiquick")
	a.win.SetIcon(AppIcon())

	combo, err := hotkey.Parse(fa.Preferences().StringWithFallback(prefHotkey, defaultHotkeyS))
	if err != nil {
		combo, _ = hotkey.Parse(defaultHotkeyS)
	}
	a.curCombo = combo

	a.buildUI()

	// 事件订阅与状态回调
	if a.cli != nil {
		a.cli.Subscribe(api.EventChunk, a.onChunk)
		a.cli.SetOnState(func(client.State) { fyne.Do(a.updateStatus) })
		a.cli.StartHeartbeat(heartInterval, heartTimeout)
	}
	return a
}

// Run 显示窗口并进入主循环（阻塞）。
func (a *App) Run() {
	a.refreshPresets()
	a.refreshConfig()
	a.win.Resize(fyne.NewSize(600, 520))
	a.win.ShowAndRun()
}

// Quit 退出：注销热键、停心跳并优雅关闭后端。
func (a *App) Quit() {
	if a.hk != nil {
		a.hk.Clear()
	}
	if a.cli != nil {
		a.cli.Shutdown()
	}
	a.fa.Quit()
}

// ShowWindow 唤起窗口并聚焦输入框（热键/托盘调用）。
func (a *App) ShowWindow() {
	a.win.Show()
	a.win.RequestFocus()
	a.win.Canvas().Focus(a.input)
}

func (a *App) buildUI() {
	a.presetSelect = widget.NewSelect([]string{}, func(name string) {
		if id := a.nameToID[name]; id != "" {
			a.fa.Preferences().SetString(prefLastPreset, id)
		}
	})
	a.presetSelect.PlaceHolder = "选择预设…"

	manageBtn := widget.NewButton("管理预设", a.openPresetManager)

	a.input = widget.NewEntry()
	a.input.PlaceHolder = "输入内容，回车发送…"
	a.input.OnSubmitted = func(string) { a.send() }

	a.sendBtn = widget.NewButton("发送", a.send)

	a.output = widget.NewLabel("")
	a.output.TextStyle = fyne.TextStyle{Monospace: true}
	a.output.Wrapping = fyne.TextWrapBreak
	a.outputScroll = container.NewScroll(a.output)

	a.copyBtn = widget.NewButton("复制结果", a.copyResult)
	a.copyBtn.Disable()

	settingsBtn := widget.NewButton("设置", a.openSettings)
	logBtn := widget.NewButton("日志", a.openLogs)

	a.statusDot = canvas.NewText("●", colorGray)
	a.statusDot.TextSize = 14
	a.statusText = widget.NewLabel("未连接")

	top := container.NewHBox(a.presetSelect, manageBtn, layout.NewSpacer())
	inputRow := container.NewBorder(nil, nil, nil, a.sendBtn, a.input)
	bottom := container.NewHBox(
		a.copyBtn, layout.NewSpacer(),
		a.statusDot, a.statusText, logBtn, settingsBtn,
	)
	a.win.SetContent(container.NewBorder(top, bottom, nil, nil,
		container.NewBorder(nil, inputRow, nil, nil, a.outputScroll)))

	// Esc 隐藏；点 X 隐藏（进程常驻托盘）
	a.win.Canvas().SetOnTypedKey(a.onTypedKey)
	a.win.SetCloseIntercept(a.win.Hide)
}

func (a *App) onTypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		a.win.Hide()
	}
}

func (a *App) focusInput() { a.win.Canvas().Focus(a.input) }

// ---- 状态与刷新 ----

func (a *App) updateStatus() {
	if a.cli == nil {
		a.statusDot.Color = colorRed
		a.statusText.SetText("后端不可用")
		return
	}
	if a.cli.State() == client.StateConnected {
		a.statusDot.Color = colorGreen
		name := a.modelName
		if name == "" {
			name = a.cli.Hello().Name
		}
		a.statusText.SetText("已连接 · " + name)
	} else {
		a.statusDot.Color = colorRed
		a.statusText.SetText("未连接，自动重连中…")
	}
	a.statusDot.Refresh()
}

// refreshConfig 拉取模型名用于状态展示。
func (a *App) refreshConfig() {
	if a.cli == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
		defer cancel()
		var cfg api.Config
		_, err := a.cli.Call(ctx, "config.get", nil, &cfg)
		fyne.Do(func() {
			if err == nil {
				a.modelName = cfg.Model
			}
			a.updateStatus()
		})
	}()
}

// refreshPresets 拉取预设列表并刷新下拉框。
func (a *App) refreshPresets() {
	if a.cli == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
		defer cancel()
		var ps []api.Preset
		_, err := a.cli.Call(ctx, "presets.list", nil, &ps)
		fyne.Do(func() {
			if err != nil {
				a.statusText.SetText("预设加载失败")
				return
			}
			a.setPresets(ps)
		})
	}()
}

func (a *App) setPresets(ps []api.Preset) {
	a.presets = ps
	a.nameToID = make(map[string]string, len(ps))
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name)
		a.nameToID[p.Name] = p.ID
	}
	prevID := a.fa.Preferences().String(prefLastPreset)
	prevName := a.presetSelect.Selected

	a.presetSelect.Options = names
	// 恢复上次选择（按 id），否则保持当前名称，否则取第一个
	switch {
	case prevID != "" && a.containsPresetID(prevID):
		if prevName == "" || a.nameToID[prevName] != prevID {
			a.presetSelect.SetSelected(a.nameByID(prevID))
		} else {
			a.presetSelect.SetSelected(prevName)
		}
	case prevName != "" && a.nameToID[prevName] != "":
		a.presetSelect.SetSelected(prevName)
	case len(names) > 0:
		a.presetSelect.SetSelected(names[0])
	}
	a.presetSelect.Refresh()
}

func (a *App) containsPresetID(id string) bool { return a.nameByID(id) != "" }

func (a *App) nameByID(id string) string {
	for _, p := range a.presets {
		if p.ID == id {
			return p.Name
		}
	}
	return ""
}

func (a *App) selectedPreset() (api.Preset, bool) {
	name := a.presetSelect.Selected
	id := a.nameToID[name]
	if id == "" {
		return api.Preset{}, false
	}
	for _, p := range a.presets {
		if p.ID == id {
			return p, true
		}
	}
	return api.Preset{}, false
}

// ---- 发送流程 ----

func (a *App) send() {
	if a.sending.Load() {
		a.cancelAsk()
		return
	}
	if a.cli == nil {
		dialog.ShowError(errors.New("后端不可用，请检查 aiquickd.exe"), a.win)
		return
	}
	preset, ok := a.selectedPreset()
	if !ok {
		dialog.ShowError(errors.New("请先选择预设"), a.win)
		a.focusInput()
		return
	}
	input := strings.TrimSpace(a.input.Text)
	if input == "" {
		dialog.ShowError(errors.New("请输入内容"), a.win)
		a.focusInput()
		return
	}

	a.sending.Store(true)
	a.curRid.Store(0)
	a.setOutput("")
	a.sendBtn.SetText("停止")
	a.copyBtn.Disable()

	ctx, cancel := context.WithCancel(context.Background())
	a.askCancel = cancel
	params := api.AskParams{PresetID: preset.ID, Input: input}

	go func() {
		var res api.AskResult
		rid, err := a.cli.CallStream(ctx, "ask.stream", params, &res, func(id int64) {
			a.curRid.Store(id)
		})
		cancel()
		fyne.Do(func() { a.finishAsk(rid, err, res.Text) })
	}()
}

func (a *App) cancelAsk() {
	if a.askCancel != nil {
		a.askCancel()
	}
}

func (a *App) finishAsk(rid int64, err error, finalText string) {
	if a.curRid.Load() == rid {
		a.curRid.Store(0)
	}
	a.sending.Store(false)
	a.sendBtn.SetText("发送")

	var pe *protocol.Error
	switch {
	case err == nil:
		if finalText != "" {
			a.setOutput(finalText) // 最终文本为权威结果
		}
		a.copyBtn.Enable()
	case errors.As(err, &pe) && pe.Code == protocol.CodeCancelled:
		a.appendOutput("\n—已取消—")
	default:
		dialog.ShowError(err, a.win)
	}
	a.focusInput()
}

// onChunk 在 client 读循环中回调：过滤 rid 后转发主线程追加。
func (a *App) onChunk(ev protocol.Event) {
	if ev.RID == 0 || ev.RID != a.curRid.Load() {
		return
	}
	var d api.ChunkData
	if err := json.Unmarshal(ev.Data, &d); err != nil || d.Text == "" {
		return
	}
	fyne.Do(func() {
		if ev.RID == a.curRid.Load() {
			a.appendOutput(d.Text)
		}
	})
}

func (a *App) setOutput(s string) {
	a.outputTxt = s
	a.output.SetText(s)
	a.outputScroll.ScrollToBottom()
}

func (a *App) appendOutput(s string) {
	a.setOutput(a.outputTxt + s)
}

func (a *App) copyResult() {
	if a.outputTxt == "" {
		return
	}
	a.fa.Clipboard().SetContent(a.outputTxt)
	a.flashButton(a.copyBtn, "已复制")
}

// flashButton 短暂改按钮文案作轻量反馈（替代 toast）。
func (a *App) flashButton(b *widget.Button, text string) {
	old := b.Text
	b.SetText(text)
	time.AfterFunc(1200*time.Millisecond, func() {
		fyne.Do(func() {
			if b.Text == text {
				b.SetText(old)
			}
		})
	})
}

func (a *App) openLogs() {
	view := widget.NewLabel(a.logs.String())
	view.TextStyle = fyne.TextStyle{Monospace: true}
	dlg := dialog.NewCustom("后端日志（最近 400 行）", "关闭",
		container.NewScroll(view), a.win)
	dlg.Resize(fyne.NewSize(620, 420))
	dlg.Show()
}
