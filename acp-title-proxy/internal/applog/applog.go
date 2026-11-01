package applog

import (
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// maxLogBytes 单个日志文件的大小上限，超过即轮转。
	maxLogBytes = 5 << 20
	// maxLogBackups 保留的历史日志份数。
	maxLogBackups = 3
	// maxLogAgeDays 历史日志的最长保留天数。
	maxLogAgeDays = 14
)

// Role 决定日志写入哪个文件。
//
// 轮转安全性取决于「写者数量」：Windows 下 Go 打开的文件句柄不带 FILE_SHARE_DELETE，
// 任何其他进程持有句柄时 rename 都会失败，因此 rename 式轮转只对单写者成立。
// 角色划分正是为了把长驻单例 daemon 与多进程客户端日志隔离到不同文件：
//   - RoleShare / RolePool 是单例 daemon，独占文件，走标准 lumberjack 轮转；
//   - RoleClient 可能被多个进程（每个 Zed panel 一个）同时追加，
//     改用「复制归档 + 原地截断」轮转（见 sharedRotator）。
type Role int

const (
	// RoleClient 是 Zed 面板拉起的客户端进程（agy share / serve / 直连代理等）。
	RoleClient Role = iota
	// RoleShare 是共享后端 daemon（顶层 share）。
	RoleShare
	// RolePool 是 AGY 预热池 daemon（顶层 daemon）。
	RolePool
)

// fileName 返回该角色对应的日志文件名。
func (r Role) fileName() string {
	switch r {
	case RoleShare:
		return "acp-title-proxy.log"
	case RolePool:
		return "acp-title-proxy-pool.log"
	default:
		return "acp-title-proxy-client.log"
	}
}

// isSingleton 表示该角色的日志只允许一个进程写入。
func (r Role) isSingleton() bool {
	return r == RoleShare || r == RolePool
}

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

// Init 将标准日志同时写入 stderr 与数据目录下的日志文件。
// 返回的函数用于刷新并关闭日志文件。
func Init(role Role) func() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	dir := DataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return func() {}
	}

	path := filepath.Join(dir, role.fileName())

	var (
		w    io.Writer
		quit func()
	)
	if role.isSingleton() {
		lj := &lumberjack.Logger{
			Filename:   path,
			MaxSize:    maxLogBytes >> 20, // 单位 MB
			MaxBackups: maxLogBackups,
			MaxAge:     maxLogAgeDays,
			Compress:   true,
			LocalTime:  true,
		}
		w, quit = lj, func() { _ = lj.Close() }
	} else {
		sr := &sharedRotator{path: path}
		if err := sr.open(); err != nil {
			return func() {}
		}
		w, quit = sr, func() { _ = sr.Close() }
	}

	log.SetOutput(io.MultiWriter(os.Stderr, w))
	return quit
}

// sharedRotator 为「多个进程同时追加同一文件」的日志提供轮转。
//
// 不用 lumberjack 的原因（已在 Windows 实测确认）：Go 的 os.OpenFile 不支持
// FILE_SHARE_DELETE，任一进程持有句柄时其他进程的 os.Rename 会返回
// ERROR_SHARING_VIOLATION，lumberjack 的 rename 式轮转在多写者下必然失败，
// 且失败后每次 Write 都会重试并丢弃日志。
//
// 这里改为「复制到带时间戳的 gzip 归档 → 原地截断」：
// 复制与截断都不需要 rename，其他进程持有的追加句柄（O_APPEND 语义）不受影响。
type sharedRotator struct {
	path string
	// limit 为 0 时使用 maxLogBytes，仅供测试注入更小的阈值。
	limit int64

	mu   sync.Mutex
	f    *os.File
	size int64
}

func (s *sharedRotator) open() error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	s.f, s.size = f, st.Size()
	return nil
}

func (s *sharedRotator) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.f == nil {
		if err := s.open(); err != nil {
			return 0, err
		}
	}
	if s.size+int64(len(p)) > s.maxBytes() {
		s.rotate()
	}

	n, err := s.f.Write(p)
	s.size += int64(n)
	return n, err
}

// maxBytes 返回触发轮转的字节阈值。
func (s *sharedRotator) maxBytes() int64 {
	if s.limit > 0 {
		return s.limit
	}
	return maxLogBytes
}

// rotate 把当前日志压缩归档后再原地截断。
// 任何一步失败都保持原文件可写，宁可继续增长也不丢日志。
func (s *sharedRotator) rotate() {
	// 独立只读句柄复制，避免干扰追加句柄的写入位置。
	src, err := os.Open(s.path)
	if err != nil {
		return
	}
	defer src.Close()

	base := strings.TrimSuffix(s.path, ".log")
	stamp := time.Now().Format("20060102-150405")
	dst := fmt.Sprintf("%s-%s-%d.log.gz", base, stamp, os.Getpid())

	out, err := os.Create(dst)
	if err != nil {
		return
	}
	gz := gzip.NewWriter(out)
	_, copyErr := io.Copy(gz, src)
	if err := gz.Close(); copyErr == nil {
		copyErr = err
	}
	if err := out.Close(); copyErr == nil {
		copyErr = err
	}
	if copyErr != nil {
		_ = os.Remove(dst)
		return
	}

	if err := os.Truncate(s.path, 0); err != nil {
		_ = os.Remove(dst)
		return
	}
	s.size = 0
	pruneArchives(s.path)
}

// pruneArchives 清理超出保留份数或过期时间的历史归档。
func pruneArchives(path string) {
	files, err := filepath.Glob(strings.TrimSuffix(path, ".log") + "-*.log.gz")
	if err != nil {
		return
	}
	sort.Strings(files)

	cutoff := time.Now().AddDate(0, 0, -maxLogAgeDays)
	excess := len(files) - maxLogBackups

	for _, name := range files {
		drop := excess > 0
		if st, err := os.Stat(name); err == nil && st.ModTime().Before(cutoff) {
			drop = true
		}
		if !drop {
			continue
		}
		if err := os.Remove(name); err == nil && excess > 0 {
			excess--
		}
	}
}

// Close 关闭日志文件。
func (s *sharedRotator) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}
