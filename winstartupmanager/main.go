package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"winstartupmanager/internal/env"
	"winstartupmanager/internal/guiapp"
	"winstartupmanager/internal/logx"
	"winstartupmanager/internal/store"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	dataDir, _ := store.DataDir()
	logx.Init(dataDir)
	defer logx.Close()

	logx.Infof("启动 version=%s commit=%s mode=%s", env.Version, env.Commit, env.ModeName())

	defer func() {
		if r := recover(); r != nil {
			logx.Errorf("程序异常退出: %v\n%s", r, debug.Stack())
			if !env.IsDev() {
				showFatalMessage(fmt.Sprintf(
					"程序遇到错误已退出。\n\n日志文件：\n%s\n\n错误信息：%v",
					filepath.Join(dataDir, logx.FileName), r,
				))
			}
			logx.Close()
			os.Exit(1)
		}
	}()

	hidden := false
	for _, arg := range os.Args[1:] {
		if arg == "--hidden" || arg == "--tray" {
			hidden = true
		}
	}

	guiapp.Run(guiapp.RunOptions{
		Hidden: hidden,
		Assets: assets,
		Icon:   iconICO,
	})
}
