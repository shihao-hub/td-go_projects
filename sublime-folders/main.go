// sublime-folders: 托盘常驻工具，定时记录 Sublime Text 打开的目录到 SQLite。
//
// Usage:
//   sublime-folders.exe                托盘模式（默认），每 5 分钟记录一次
//   sublime-folders.exe -interval 3m   自定义采集间隔
//   sublime-folders.exe -no-tray       无托盘模式（调试用），仅采集入库
//
// 数据与日志: %APPDATA%\sublime-folders\
// 托盘菜单: 查看当前目录 / 最新 10 条记录 / 全部记录 / 打开数据目录 / 退出
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"
)

const (
	defaultInterval = 5 * time.Minute
	retainDays      = 30
)

func main() {
	interval := flag.Duration("interval", defaultInterval, "采集间隔（如 5m、30s）")
	noTray := flag.Bool("no-tray", false, "无托盘模式，仅采集入库")
	flag.Parse()

	initLogging()

	if !acquireSingleInstance() {
		log.Printf("已有实例在运行，退出")
		return
	}

	st, err := openStore()
	if err != nil {
		log.Printf("打开数据库失败: %v", err)
		alertError("sublime-folders 启动失败", "打开数据库失败: "+err.Error())
		return
	}
	defer st.Close()
	log.Printf("启动: interval=%s retain=%dd no-tray=%v", *interval, retainDays, *noTray)

	if *noTray {
		captureLoop(st, *interval, retainDays)
		return
	}
	runTray(st, *interval, retainDays)
	log.Printf("已退出")
}

func initLogging() {
	dir, err := dataDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "sublime-folders.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags)
}
