package main

import (
	_ "embed"
	"fmt"
	"log"
	"os/exec"

	"github.com/getlantern/systray"
)

//go:embed assets/icon.ico
var iconBytes []byte

func runTray(a *app) {
	systray.Run(func() {
		systray.SetIcon(iconBytes)
		systray.SetTooltip(fmt.Sprintf("zread browse · %s", a.Dir()))

		mStatus := systray.AddMenuItem("状态：启动中…", "zread browse 运行状态")
		mStatus.Disable()
		systray.AddSeparator()
		mSwitch := systray.AddMenuItem("切换工作区…", "选择其他目录并重启 zread browse")
		mRestart := systray.AddMenuItem("重启 zread", "重新启动 zread browse")
		mLog := systray.AddMenuItem("查看日志", fmt.Sprintf("打开 %s", a.logPath))
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "结束 zread browse 并退出")

		go func() {
			for {
				select {
				case <-mSwitch.ClickedCh:
					chosen, err := pickFolder(a.Dir())
					if err != nil {
						alertError("zread-tray 切换工作区失败", err.Error())
						continue
					}
					if chosen == "" || chosen == a.Dir() {
						continue
					}
					if err := a.SwitchDir(chosen); err != nil {
						alertError("zread-tray 切换工作区失败", err.Error())
						continue
					}
					if err := saveConfig(config{LastDir: chosen}); err != nil {
						log.Printf("记住上次工作区失败: %v", err)
					}
					systray.SetTooltip(fmt.Sprintf("zread browse · %s", a.Dir()))
				case <-mRestart.ClickedCh:
					if err := a.Restart(); err != nil {
						alertError("zread-tray 重启失败", err.Error())
					}
				case <-mLog.ClickedCh:
					openLog(a.logPath)
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()

		go func() {
			for range a.Events() {
				if a.Alive() {
					mStatus.SetTitle("状态：运行中")
				} else {
					mStatus.SetTitle("状态：已退出（详见日志）")
				}
			}
		}()
	}, func() {
		a.Stop()
	})
}

func openLog(path string) {
	_ = exec.Command("notepad", path).Start()
}
