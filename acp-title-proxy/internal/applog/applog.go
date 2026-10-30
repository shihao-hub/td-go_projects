package applog

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// maxLogBytes 触发日志轮转的文件大小上限。
const maxLogBytes = 5 << 20

var (
	mu   sync.Mutex
	file *os.File
)

// DataDir 返回符合仓库数据落盘规范的项目数据目录。
func DataDir() string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, "language_projects", "acp-title-proxy")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".language_projects", "acp-title-proxy")
}

// Init 将标准日志同时写入 stderr 与数据目录下的 acp-title-proxy.log。
// 返回的函数用于刷新并关闭日志文件。
func Init() func() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	dir := DataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return func() {}
	}

	path := filepath.Join(dir, "acp-title-proxy.log")
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogBytes {
		_ = os.Rename(path, path+".old")
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return func() {}
	}

	mu.Lock()
	file = f
	mu.Unlock()

	log.SetOutput(io.MultiWriter(os.Stderr, &lockedWriter{w: f}))

	return func() {
		mu.Lock()
		defer mu.Unlock()
		if file != nil {
			_ = file.Close()
			file = nil
		}
	}
}

// lockedWriter 串行化进程内对日志文件的写入，避免多行交错。
type lockedWriter struct {
	w io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	mu.Lock()
	defer mu.Unlock()
	return l.w.Write(p)
}
