package ui

import (
	"context"
	"errors"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"aiquick/internal/api"
	"aiquick/internal/hotkey"
)

// openSettings 设置对话框：API 配置 / 划词开关 / 全局热键（捕获式）。
func (a *App) openSettings() {
	var cfg api.Config
	if a.cli != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
		defer cancel()
		_, _ = a.cli.Call(ctx, "config.get", nil, &cfg) // 失败则表单留空，保存时仍可写
	}

	baseEntry := widget.NewEntry()
	baseEntry.SetPlaceHolder("https://open.bigmodel.cn/api/paas/v4")
	baseEntry.SetText(cfg.BaseURL)

	keyEntry := widget.NewPasswordEntry()
	keyEntry.SetPlaceHolder("API Key")
	keyEntry.SetText(cfg.APIKey)

	modelEntry := widget.NewEntry()
	modelEntry.SetPlaceHolder("glm-4.7-flash")
	modelEntry.SetText(cfg.Model)

	captureSel := widget.NewSelect([]string{"开", "关"}, nil)
	if a.fa.Preferences().BoolWithFallback(prefCapture, true) {
		captureSel.SetSelected("开")
	} else {
		captureSel.SetSelected("关")
	}

	// 热键：三个修饰键勾选 + 点击后按下一个主键完成捕获
	combo := a.curCombo
	hotkeyLbl := widget.NewLabel(combo.String())
	altChk := widget.NewCheck("Alt", nil)
	altChk.SetChecked(combo.Alt)
	ctrlChk := widget.NewCheck("Ctrl", nil)
	ctrlChk.SetChecked(combo.Ctrl)
	shiftChk := widget.NewCheck("Shift", nil)
	shiftChk.SetChecked(combo.Shift)

	captureBtn := widget.NewButton("点击捕获主键", nil)
	armed := false
	disarm := func() {
		armed = false
		captureBtn.SetText("点击捕获主键")
	}
	captureBtn.OnTapped = func() {
		if armed {
			disarm()
			return
		}
		armed = true
		captureBtn.SetText("请按下主键（Esc 取消）…")
	}

	a.win.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if !armed {
			a.onTypedKey(ev) // 对话框开着也保留 Esc 收起主窗口
			return
		}
		if ev.Name == fyne.KeyEscape {
			disarm()
			return
		}
		key := string(ev.Name)
		if !hotkey.ValidKey(key) {
			return // 无效主键继续等待
		}
		combo = hotkey.Combo{Alt: altChk.Checked, Ctrl: ctrlChk.Checked, Shift: shiftChk.Checked, Key: key}
		if !combo.Valid() {
			captureBtn.SetText("需至少勾选一个修饰键")
			time.AfterFunc(1500*time.Millisecond, disarm)
			return
		}
		hotkeyLbl.SetText(combo.String())
		disarm()
	})

	dlg := dialog.NewForm("设置", "保存", "取消", []*widget.FormItem{
		widget.NewFormItem("BaseURL", baseEntry),
		widget.NewFormItem("API Key", keyEntry),
		widget.NewFormItem("模型", modelEntry),
		widget.NewFormItem("唤起时划词预填", captureSel),
		widget.NewFormItem("全局热键", container.NewVBox(
			container.NewHBox(altChk, ctrlChk, shiftChk, captureBtn),
			hotkeyLbl,
		)),
	}, func(ok bool) {
		// 恢复默认按键处理（Esc 收起主窗口）
		a.win.Canvas().SetOnTypedKey(a.onTypedKey)
		if !ok {
			return
		}
		a.saveSettings(baseEntry.Text, keyEntry.Text, modelEntry.Text,
			captureSel.Selected == "开", combo)
	}, a.win)
	dlg.Resize(fyne.NewSize(520, 460))
	dlg.Show()
}

func (a *App) saveSettings(baseURL, apiKey, model string, captureOn bool, combo hotkey.Combo) {
	prefs := a.fa.Preferences()

	// 热键变更：立即重注册，失败提示占用
	if combo != a.curCombo {
		if err := a.hk.Set(combo, a.onHotkey); err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		a.curCombo = combo
		prefs.SetString(prefHotkey, combo.String())
	}
	prefs.SetBool(prefCapture, captureOn)

	// API 配置：有后端才保存
	if a.cli != nil {
		cfg := api.Config{BaseURL: baseURL, APIKey: apiKey, Model: model}
		ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
		defer cancel()
		if _, err := a.cli.Call(ctx, "config.set", cfg, nil); err != nil {
			dialog.ShowError(errors.New("保存配置失败: "+err.Error()), a.win)
			return
		}
		a.modelName = model
		a.updateStatus()
	}
	dialog.ShowInformation("设置", "已保存", a.win)
}
