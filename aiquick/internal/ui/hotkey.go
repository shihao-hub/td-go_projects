package ui

import (
	"fyne.io/fyne/v2"

	"aiquick/internal/capture"
)

// onHotkey 全局热键回调（运行在热键线程）：
// 可选划词抓取 → 主线程唤起窗口并预填。
func (a *App) onHotkey() {
	var text string
	if a.fa.Preferences().BoolWithFallback(prefCapture, true) {
		text, _ = capture.SelectedText(captureMaxRunes)
	}
	fyne.Do(func() {
		a.ShowWindow()
		if text != "" {
			a.input.SetText(text)
		}
	})
}

// RegisterInitialHotkey 启动时注册当前配置的热键；失败记入日志（不弹窗打扰）。
func (a *App) RegisterInitialHotkey() {
	if a.hk == nil {
		return
	}
	if err := a.hk.Set(a.curCombo, a.onHotkey); err != nil {
		a.logs.AppendString("全局热键注册失败: " + err.Error())
	}
}
