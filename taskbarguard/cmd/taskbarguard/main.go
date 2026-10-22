//go:build !cli

// GUI 通道入口（taskbarguard-gui）：只负责把桌面工作台拉起来。
//
// 它不承担任何 CLI 语义：不管从哪里启动（双击、终端、快捷方式、带任意参数），
// 行为都是显示 GUI。唯一有意义的参数是 --hidden（开机自启的静默托盘模式，
// --tray 为同义别名），其余参数一律忽略。需要命令行能力请改用
// console 通道产物 taskbarguard-cli.exe。
package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"taskbarguard/internal/api"
	"taskbarguard/internal/app"
	"taskbarguard/internal/buildinfo"
	"taskbarguard/internal/core"
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

	if err := app.LaunchGUI(0, hidden); err != nil {
		// 桌面壳起不来（典型原因：WebView2 运行时缺失）时降级为无窗口 HTTP 服务，
		// 至少还能用浏览器打开工作台；诊断信息已由 app.logf 落盘。
		fallbackServe(!hidden)
	}
}

// fallbackServe 桌面壳不可用时的兜底：起内置 HTTP 服务，可选自动打开浏览器。
func fallbackServe(openBrowser bool) {
	paths := core.ResolvePaths(buildinfo.Channel(), "", "")
	service := core.New(paths)

	listener, err := api.Listen(0)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "启动 HTTP 监听失败: %v\n", err)
		return
	}
	url := api.URL(listener)
	_, _ = fmt.Fprintf(os.Stderr, "TaskbarGuard WebUI is running at %s\n", url)
	if openBrowser {
		_ = launchBrowser(url)
	}

	server := api.NewServer(service, api.Meta{
		Channel:    buildinfo.Channel(),
		Version:    buildinfo.Version,
		BuildMode:  buildinfo.BuildMode,
		ConfigPath: paths.ConfigPath,
		ScriptsDir: paths.ScriptsDir,
		DataDir:    paths.DataDir,
		TaskName:   core.ScheduledTask,
	})
	_ = http.Serve(listener, server.Handler())
}

func launchBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
	default:
		cmd = "xdg-open"
	}
	return exec.Command(cmd, args...).Start()
}
