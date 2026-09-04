// aiquick 是 AI 快速助手的界面进程：
// 常驻托盘，全局热键(默认 Alt+S)秒开窗口，选预设 → 输入 → 回车 → 流式看结果。
// 业务全部由子进程 aiquickd.exe 承担，本进程只做展示与交互。
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"

	"aiquick/internal/client"
	"aiquick/internal/hotkey"
	"aiquick/internal/ui"
)

func main() {
	fa := app.NewWithID("aiquick")
	fa.SetIcon(ui.AppIcon())

	logs := ui.NewLogBuffer(400)

	// 拉起后端
	var cli *client.Client
	path, err := client.ResolveBackend("")
	if err != nil {
		logs.AppendString("未找到 aiquickd.exe: " + err.Error())
	} else {
		cli, err = client.Start(path, logs)
		if err != nil {
			logs.AppendString("aiquickd 启动失败: " + err.Error())
			cli = nil
		}
	}

	a := ui.New(fa, cli, logs, hotkey.NewManager())

	// 托盘：显示 / 退出
	if desk, ok := fa.(desktop.App); ok {
		menu := fyne.NewMenu("aiquick",
			fyne.NewMenuItem("显示主窗口", func() { fyne.Do(a.ShowWindow) }),
			fyne.NewMenuItem("退出", a.Quit),
		)
		desk.SetSystemTrayMenu(menu)
	}

	a.RegisterInitialHotkey()
	a.Run()
}
