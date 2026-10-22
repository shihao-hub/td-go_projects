//go:build !cli

// GUI 通道入口（plansql-gui）：只负责把桌面工作台拉起来。
//
// 它不承担任何 CLI 语义：不管从哪里启动（双击、终端、快捷方式、带任意参数），
// 行为都是显示 GUI。唯一有意义的参数是 --hidden（开机自启注册的静默托盘模式，
// --tray 为同义别名），其余参数一律忽略。
// 需要命令行能力请改用 console 通道产物 plansql-cli.exe。
package main

import (
	"fmt"
	"os"
	"plansql/internal/app"
	"plansql/internal/daemon"
)

func main() {
	defer app.Recover(true)

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
	root, _ := app.ResolveInitialProjectRoot()
	server, err := daemon.NewServer(daemon.Config{
		Addr:       "127.0.0.1:0",
		RootDir:    root,
		NoAddrFile: true,
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "初始化项目服务失败: %v\n", err)
		return
	}

	listener, err := daemon.Listen("127.0.0.1:0")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "启动 HTTP 监听失败: %v\n", err)
		return
	}

	url := daemon.URL(listener)
	fmt.Printf("plansql WebUI is running at %s\n", url)
	if openBrowser {
		_ = app.OpenBrowser(url)
	}

	if err := server.Serve(listener); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "服务异常终止: %v\n", err)
	}
}
