package cli

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"typeai/internal/api"
	"typeai/internal/appdata"
	"typeai/internal/browser"
	"typeai/internal/buildinfo"
	"typeai/internal/config"
	"typeai/internal/service"
)

// cmdServe 启动内嵌 WebUI 与 REST/SSE 服务，供浏览器或远程客户端使用。
// 与 GUI 共用同一 api.Handler()，根路径必须是完整工作台而不是裸 API。
func cmdServe(args []string) int {
	fs := flag.NewFlagSet("typeai serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	port := fs.Int("port", 0, "监听端口；0 表示由内核动态探测")
	open := fs.Bool("open", false, "启动后自动打开默认浏览器")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "typeai: serve 不支持位置参数:", fs.Arg(0))
		return 2
	}

	dir, err := appdata.Dir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "typeai:", err)
		return 1
	}
	cfg, err := config.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "typeai:", err)
		return 1
	}
	chat, err := service.NewChat(cfg, dir, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "typeai:", err)
		return 1
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "typeai: 绑定监听失败:", err)
		return 1
	}
	url := "http://" + listener.Addr().String()
	fmt.Printf("typeai WebUI is running at %s\n", url)
	if *open {
		if err := browser.Open(url); err != nil {
			fmt.Fprintln(os.Stderr, "typeai: 打开浏览器失败:", err)
		}
	}

	server := api.NewServer(api.Options{
		Chat:    chat,
		Config:  cfg,
		DataDir: dir,
		Version: Version,
		Channel: buildinfo.Channel(),
	})
	if err := http.Serve(listener, server.Handler()); err != nil {
		fmt.Fprintln(os.Stderr, "typeai: 服务异常终止:", err)
		return 1
	}
	return 0
}
