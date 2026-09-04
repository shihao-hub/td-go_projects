package ui

import (
	"context"
	"errors"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"aiquick/internal/api"
)

// openPresetManager 预设管理对话框：列表 + 新建/编辑/删除。
func (a *App) openPresetManager() {
	if a.cli == nil {
		dialog.ShowError(errors.New("后端不可用"), a.win)
		return
	}

	data := append([]api.Preset(nil), a.presets...)
	var list *widget.List

	reload := func() {
		var ps []api.Preset
		ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
		defer cancel()
		if _, err := a.cli.Call(ctx, "presets.list", nil, &ps); err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		data = ps
		list.Refresh()
		a.setPresets(ps) // 按钮回调在主线程，可直接刷新主界面下拉
	}

	list = widget.NewList(
		func() int { return len(data) },
		func() fyne.CanvasObject {
			return container.NewBorder(nil, nil, nil,
				container.NewHBox(
					widget.NewButton("编辑", nil),
					widget.NewButton("删除", nil),
				),
				widget.NewLabel("模板"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(data) {
				return
			}
			p := data[id]
			border := obj.(*fyne.Container)
			name := border.Objects[0].(*widget.Label)
			name.SetText(p.Name)
			btns := border.Objects[1].(*fyne.Container)
			edit := btns.Objects[0].(*widget.Button)
			del := btns.Objects[1].(*widget.Button)
			edit.OnTapped = func() { a.editPresetDialog(&p, reload) }
			del.OnTapped = func() {
				dialog.NewConfirm("删除预设", "确定删除「"+p.Name+"」？", func(ok bool) {
					if !ok {
						return
					}
					ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
					defer cancel()
					if _, err := a.cli.Call(ctx, "presets.delete", api.PresetIDParams{ID: p.ID}, nil); err != nil {
						dialog.ShowError(err, a.win)
						return
					}
					reload()
				}, a.win).Show()
			}
		},
	)

	newBtn := widget.NewButton("新建预设", func() { a.editPresetDialog(nil, reload) })

	dlg := dialog.NewCustom("管理预设", "关闭",
		container.NewBorder(nil, newBtn, nil, nil, list), a.win)
	dlg.Resize(fyne.NewSize(520, 440))
	dlg.Show()
}

// editPresetDialog 新建（p == nil）或编辑一个预设。
func (a *App) editPresetDialog(p *api.Preset, onSaved func()) {
	var preset api.Preset
	if p != nil {
		preset = *p
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("预设名称，如：取变量名")
	nameEntry.SetText(preset.Name)

	sysEntry := widget.NewMultiLineEntry()
	sysEntry.SetPlaceHolder("给 AI 的指令（system），如：你是资深程序员，根据需求返回 5 个英文变量名候选…")
	sysEntry.SetText(preset.System)
	sysEntry.SetMinRowsVisible(5)

	tplEntry := widget.NewMultiLineEntry()
	tplEntry.SetPlaceHolder("可选。用户消息模板，用 {{input}} 代表输入；留空则输入即完整消息")
	tplEntry.SetText(preset.UserTemplate)
	tplEntry.SetMinRowsVisible(3)

	form := widget.NewForm(
		widget.NewFormItem("名称", nameEntry),
		widget.NewFormItem("指令", sysEntry),
		widget.NewFormItem("模板", tplEntry),
	)

	var dlg dialog.Dialog
	var saveBtn *widget.Button
	saveBtn = widget.NewButton("保存", func() {
		preset.Name = strings.TrimSpace(nameEntry.Text)
		preset.System = strings.TrimSpace(sysEntry.Text)
		preset.UserTemplate = strings.TrimSpace(tplEntry.Text)
		if preset.Name == "" || preset.System == "" {
			a.flashButton(saveBtn, "名称与指令不能为空")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
		defer cancel()
		var saved api.Preset
		if _, err := a.cli.Call(ctx, "presets.save", preset, &saved); err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		onSaved()
		if dlg != nil {
			dlg.Hide()
		}
		dialog.ShowInformation("预设", "已保存", a.win)
	})

	dlg = dialog.NewCustom("编辑预设", "关闭",
		container.NewVBox(form, saveBtn), a.win)
	dlg.Resize(fyne.NewSize(520, 460))
	dlg.Show()
}
