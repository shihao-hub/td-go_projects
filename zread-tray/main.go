package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
)

func main() {
	dir := flag.String("dir", "", "zread 工作区目录（留空则启动时弹出选择框，并记住上次选择）")
	host := flag.String("host", "", "zread browse 监听地址（默认由 zread 决定）")
	port := flag.Int("port", 0, "监听端口（0 表示由 zread 从 9681 起自动探测）")
	generate := flag.Bool("generate", false, "工作区无文档时直接启动生成流程而非显示菜单")
	noTray := flag.Bool("no-tray", false, "以控制台模式运行，不显示系统托盘图标")
	flag.Parse()

	initLogging()
	log.Printf("启动: dir=%q host=%q port=%d generate=%v no-tray=%v", *dir, *host, *port, *generate, *noTray)

	target := *dir
	if target == "" {
		cfg := loadConfig()
		log.Printf("上次工作区: %q", cfg.LastDir)
		chosen, err := pickFolder(cfg.LastDir)
		if err != nil {
			log.Printf("目录选择框失败: %v", err)
			alertError("zread-tray 启动失败", "打开目录选择框失败: "+err.Error())
			return
		}
		if chosen == "" {
			log.Printf("未选择工作区，退出")
			return
		}
		target = chosen
	}

	absDir, err := filepath.Abs(target)
	if err != nil {
		log.Fatalf("解析工作区目录失败: %v", err)
	}

	app, err := newApp(appOptions{Dir: absDir, Host: *host, Port: *port, Generate: *generate})
	if err != nil {
		log.Printf("启动 zread 失败: %v", err)
		alertError("zread-tray 启动失败", err.Error())
		return
	}
	log.Printf("zread browse 已启动: dir=%s", absDir)

	if err := saveConfig(config{LastDir: absDir}); err != nil {
		log.Printf("记住上次工作区失败: %v", err)
	}

	if *noTray {
		app.Wait()
		return
	}
	runTray(app)
}

func initLogging() {
	base, err := os.UserConfigDir()
	if err != nil {
		return
	}
	dir := filepath.Join(base, "zread-tray")
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "zread-tray.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags)
}
