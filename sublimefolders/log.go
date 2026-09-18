package app

import (
	"log"
	"os"
	"path/filepath"
)

// InitLogging 把日志重定向到 %APPDATA%\sublimefolders\sublimefolders.log。
func InitLogging() {
	dir, err := DataDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "sublimefolders.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags)
}
