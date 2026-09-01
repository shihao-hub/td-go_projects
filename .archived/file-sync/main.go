package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"strings"

	"file-sync/config"
	"file-sync/logging"
	"file-sync/web"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP 监听地址")
	cfgPath := flag.String("config", "", "配置文件路径（默认 ~/.file-sync/config.json）")
	logPath := flag.String("log", "", "日志文件路径（默认 ~/.file-sync/file-sync.log）")
	noTray := flag.Bool("no-tray", false, "以控制台模式运行，不显示系统托盘图标")
	flag.Parse()

	if err := logging.Init(*logPath); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		logging.Errorf("main", "加载配置失败: %v", err)
		log.Fatalf("加载配置失败: %v", err)
	}

	logging.Infof("main", "File Sync 启动: 配置文件=%s 日志文件已就绪 任务数=%d", cfg.Path(), len(cfg.ListTasks()))

	display := *addr
	if strings.HasPrefix(display, ":") {
		display = "localhost" + display
	}
	url := "http://" + display
	logging.Infof("main", "监听地址: %s", url)

	srv, err := web.NewServer(cfg, *addr)
	if err != nil {
		logging.Errorf("main", "初始化服务器失败: %v", err)
		log.Fatalf("初始化服务器失败: %v", err)
	}

	if *noTray {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logging.Errorf("main", "服务器退出: %v", err)
			log.Fatalf("服务器退出: %v", err)
		}
		return
	}

	fail := make(chan struct{})
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logging.Errorf("main", "服务器异常退出: %v", err)
			close(fail)
		}
	}()

	runTray(srv, url, fail)
}
