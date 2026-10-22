//go:build !cli

// GUI 通道入口（typeai-gui）：只负责把桌面工作台拉起来。
//
// 不承担任何 CLI 语义：不管从哪里启动（双击、终端、快捷方式、带任意参数），
// 行为都是显示 GUI。唯一有意义的参数是 --hidden（开机自启注册的静默托盘模式，
// --tray 为同义别名），其余参数一律忽略。
// 需要命令行能力请改用 console 通道产物 typeai-cli.exe。
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"typeai/internal/api"
	"typeai/internal/app"
	"typeai/internal/appdata"
	"typeai/internal/browser"
	"typeai/internal/buildinfo"
	"typeai/internal/config"
	"typeai/internal/service"
)

func main() {
	hidden := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--hidden", "--tray":
			hidden = true
		}
	}

	if err := app.LaunchGUI(hidden); err != nil {
		// 桌面壳起不来（典型原因：WebView2 运行时缺失）时降级为无窗口 HTTP 服务，
		// 至少还能用浏览器打开工作台；诊断信息已由 app.logf 落盘。
		fallbackServe(!hidden)
	}
}

// fallbackServe 桌面壳不可用时的兜底：起内置 HTTP 服务，可选自动打开浏览器。
func fallbackServe(openBrowser bool) {
	dir, err := appdata.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化数据目录失败: %v\n", err)
		return
	}
	cfg, err := config.Load(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取配置失败: %v\n", err)
		return
	}
	chat, err := service.NewChat(cfg, dir, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化会话失败: %v\n", err)
		return
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "启动 HTTP 监听失败: %v\n", err)
		return
	}
	url := "http://" + listener.Addr().String()
	fmt.Printf("typeai WebUI is running at %s\n", url)
	if openBrowser {
		_ = browser.Open(url)
	}

	server := api.NewServer(api.Options{
		Chat:    chat,
		Config:  cfg,
		DataDir: dir,
		Version: buildinfo.Version,
		Channel: buildinfo.Channel(),
	})
	if err := http.Serve(listener, server.Handler()); err != nil {
		fmt.Fprintf(os.Stderr, "服务异常终止: %v\n", err)
	}
}
