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

	"ccquota/internal/appenv"
	"ccquota/internal/server"
	"ccquota/internal/service"
)

// runWeb 启动 WebUI：默认 127.0.0.1:0 由内核分配端口，打印实际地址并写 runtime.json。
func runWeb(args []string, opt *Options) int {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(opt.Stderr)
	addr := fs.String("addr", "127.0.0.1:0", "监听地址（:0 表示由系统分配端口）")
	open := fs.Bool("open", false, "启动后打开默认浏览器")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	svc, err := service.New(opt.DBPath)
	if err != nil {
		fmt.Fprintf(opt.Stderr, "ccquota: %v\n", err)
		return 1
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(opt.Stderr, "ccquota: 监听失败: %v\n", err)
		return 1
	}
	url := "http://" + listener.Addr().String()
	fmt.Fprintf(opt.Stdout, "ccquota WebUI: %s\n", url)

	pid := os.Getpid()
	if err := appenv.WriteRuntime(appenv.Runtime{URL: url, PID: pid, Mode: "web"}); err != nil {
		fmt.Fprintf(opt.Stderr, "ccquota: 写入 runtime.json 失败: %v\n", err)
	}
	defer appenv.RemoveRuntime(pid)

	if *open {
		go openBrowser(url)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{Handler: server.Handler(svc)}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(opt.Stderr, "ccquota: 服务异常终止: %v\n", err)
		return 1
	}
	return 0
}

// openBrowser 用系统默认程序打开 URL。
func openBrowser(url string) {
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
	_ = exec.Command(cmd, args...).Start()
}
