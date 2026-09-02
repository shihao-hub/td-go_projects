package app

import (
	_ "embed"
	"fmt"
	"log"
	"os/exec"
	"time"

	"github.com/getlantern/systray"
)

//go:embed assets/icon.ico
var iconBytes []byte

func RunTray(st *Store, interval time.Duration, retainDays int) {
	systray.Run(func() {
		systray.SetIcon(iconBytes)
		systray.SetTooltip(fmt.Sprintf("sublime-folders · 每 %s 记录一次 Sublime 打开的目录", interval))

		mStatus := systray.AddMenuItem("sublime-folders · 运行中", "定时记录 Sublime Text 打开的目录")
		mStatus.Disable()
		systray.AddSeparator()
		mCurrent := systray.AddMenuItem("查看当前目录", "实时读取当前打开的目录")
		mLatest := systray.AddMenuItem("最新 10 条记录", "最近 10 次目录快照")
		mAll := systray.AddMenuItem("全部记录", "查看全部历史记录")
		systray.AddSeparator()
		mData := systray.AddMenuItem("打开数据目录", "记录库与日志所在目录")
		mQuit := systray.AddMenuItem("退出", "退出 sublime-folders")

		go CaptureLoop(st, interval, retainDays)

		go func() {
			for {
				select {
				case <-mCurrent.ClickedCh:
					showCurrent()
				case <-mLatest.ClickedCh:
					snaps, err := st.latestN(10)
					if err != nil {
						AlertError("sublime-folders", "查询记录失败: "+err.Error())
						continue
					}
					showRecords("Sublime Text 目录记录 · 最新 10 条", "records-latest", snaps)
				case <-mAll.ClickedCh:
					snaps, err := st.all()
					if err != nil {
						AlertError("sublime-folders", "查询记录失败: "+err.Error())
						continue
					}
					showRecords("Sublime Text 目录记录 · 全部", "records-all", snaps)
				case <-mData.ClickedCh:
					if dir, err := DataDir(); err == nil {
						_ = exec.Command("explorer", dir).Start()
					}
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}, func() {
		log.Printf("退出托盘")
	})
}
