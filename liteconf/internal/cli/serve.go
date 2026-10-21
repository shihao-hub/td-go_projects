package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/shihao-hub/liteconf/internal/appenv"
	"github.com/shihao-hub/liteconf/internal/mcp"
	"github.com/shihao-hub/liteconf/internal/server"
)

const servePollInterval = 5 * time.Second

// defaultServerURL 未发现运行中实例时的回退地址（与 MCP 默认值同源）。
const defaultServerURL = mcp.DefaultServerURL

// runServe 启动与 GUI 完全相同的 HTTP 数据面（WebUI + REST），无窗口常驻。
// 默认绑定 127.0.0.1:0 由内核动态探测端口，实际地址打印到 stdout 并写入
// runtime.json，供 MCP 与后续 CLI 调用自动发现。
func runServe(args []string, opt *Options) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(opt.Stderr)
	addr := fs.String("addr", "127.0.0.1:0", "HTTP 监听地址（:0 = 由系统动态探测端口）")
	root := fs.String("root", appenv.ConfigRoot(), "配置根目录（configs/{app}/{env}.json）")
	poll := fs.Duration("poll", servePollInterval, "外部编辑检测间隔")
	open := fs.Bool("open", false, "启动后打开系统默认浏览器")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(opt.Stderr, "liteconf serve: 参数解析失败")
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(opt.Stderr, "liteconf serve: 未知参数 %q\n", fs.Arg(0))
		return 2
	}
	if *poll <= 0 {
		*poll = servePollInterval
	}

	svc, err := server.NewService(*root, *poll)
	if err != nil {
		fmt.Fprintf(opt.Stderr, "liteconf serve: 加载配置失败：%v\n", err)
		return 1
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(opt.Stderr, "liteconf serve: 绑定监听失败：%v\n", err)
		return 1
	}
	url := "http://" + listener.Addr().String()
	fmt.Fprintf(opt.Stdout, "liteconf WebUI is running at %s\n", url)
	fmt.Fprintf(opt.Stdout, "config root: %s\n", *root)

	if err := appenv.WriteRuntime(appenv.Runtime{
		URL:  url,
		PID:  os.Getpid(),
		Mode: "serve",
	}); err != nil {
		fmt.Fprintf(opt.Stderr, "liteconf serve: 写入 runtime.json 失败：%v\n", err)
	}
	pid := os.Getpid()
	defer appenv.RemoveRuntime(pid)

	if *open {
		go func() { _ = openBrowserURL(url) }()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go svc.RunPoller(ctx)

	srv := &http.Server{Handler: svc.Handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(opt.Stderr, "liteconf serve: 服务异常终止：%v\n", err)
		return 1
	}
	return 0
}

// runURL 输出当前可用的服务地址：优先读取运行中 GUI/serve 写入的 runtime.json，
// 没有发现时回退默认地址。供 `http` 透传命令与脚本拼接 URL 使用。
func runURL(opt *Options) int {
	if rt, ok := appenv.DiscoverRuntime(); ok {
		fmt.Fprintln(opt.Stdout, rt.URL)
		return 0
	}
	fmt.Fprintln(opt.Stdout, defaultServerURL)
	return 0
}

func openBrowserURL(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	return exec.Command(cmd, args...).Start()
}
