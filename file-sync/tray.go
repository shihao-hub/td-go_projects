package main

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"file-sync/logging"
	"file-sync/web"

	"github.com/getlantern/systray"
	"github.com/pkg/browser"
)

//go:embed assets/icon.ico
var iconBytes []byte

func runTray(srv *web.Server, url string, fail <-chan struct{}) {
	systray.Run(func() {
		systray.SetIcon(iconBytes)
		systray.SetTooltip(fmt.Sprintf("File Sync 运行中 · %s", url))

		mOpen := systray.AddMenuItem("打开 Web 界面", fmt.Sprintf("在浏览器中打开 %s", url))
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 File Sync")

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					if err := browser.OpenURL(url); err != nil {
						logging.Errorf("tray", "打开浏览器失败: %v", err)
					}
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()

		select {
		case <-fail:
			alertError("File Sync 启动失败", "HTTP 服务启动异常，端口可能被占用，详见日志文件。")
			systray.Quit()
		default:
			go func() {
				<-fail
				alertError("File Sync 运行异常", "HTTP 服务已停止，详见日志文件。")
				systray.Quit()
			}()
		}
	}, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logging.Errorf("tray", "关闭 HTTP 服务异常: %v", err)
		}
		logging.Infof("tray", "File Sync 已退出")
	})
}
