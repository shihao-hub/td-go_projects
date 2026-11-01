package applog

import (
	"compress/gzip"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fileTime 返回 daysAgo 天前的时间点。
func fileTime(daysAgo int) time.Time {
	return time.Now().AddDate(0, 0, -daysAgo)
}

// tryReadGz 尝试读取 gzip 归档，压缩未完成时返回错误。
func tryReadGz(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	b, err := io.ReadAll(gz)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// readGz 读取 gzip 归档的明文内容。
func readGz(t *testing.T, path string) string {
	t.Helper()
	got, err := tryReadGz(path)
	if err != nil {
		t.Fatalf("read gzip %s: %v", path, err)
	}
	return got
}

// TestSharedRotatorRotates 验证超限后生成 gzip 归档并原地截断，且日志不丢。
func TestSharedRotatorRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.log")

	r := &sharedRotator{path: path, limit: 64}
	if err := r.open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	first := strings.Repeat("A", 40) + "\n"
	second := strings.Repeat("B", 40) + "\n"
	if _, err := r.Write([]byte(first)); err != nil {
		t.Fatalf("write first: %v", err)
	}
	// 第二次写入越过阈值，应先归档第一段再写第二段。
	if _, err := r.Write([]byte(second)); err != nil {
		t.Fatalf("write second: %v", err)
	}

	if got, err := os.ReadFile(path); err != nil || string(got) != second {
		t.Fatalf("current log = %q, err = %v; want only %q", string(got), err, second)
	}

	archives, _ := filepath.Glob(filepath.Join(dir, "client-*.log.gz"))
	if len(archives) != 1 {
		t.Fatalf("archives = %v, want exactly 1", archives)
	}
	if got := readGz(t, archives[0]); got != first {
		t.Fatalf("archive content = %q, want %q", got, first)
	}
}

// TestSharedRotatorPrunesBackups 验证归档数量受 maxLogBackups 限制。
func TestSharedRotatorPrunesBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.log")

	r := &sharedRotator{path: path, limit: 16}
	if err := r.open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	// 连续写 6 次，触发多次轮转，归档数应被裁剪到 maxLogBackups。
	for i := 0; i < 6; i++ {
		if _, err := r.Write([]byte(strings.Repeat("x", 12) + "\n")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	archives, _ := filepath.Glob(filepath.Join(dir, "client-*.log.gz"))
	if len(archives) > maxLogBackups {
		t.Fatalf("archives = %d, want <= %d (%v)", len(archives), maxLogBackups, archives)
	}
}

// TestPruneArchivesDropsExpired 验证过期归档会被删除，即使份数未超限。
func TestPruneArchivesDropsExpired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.log")

	old := filepath.Join(dir, "client-20200101-000000-1.log.gz")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old archive: %v", err)
	}
	if err := os.Chtimes(old, fileTime(30), fileTime(30)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	pruneArchives(path)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("expired archive still exists, err = %v", err)
	}
}

// TestInitShareRotates 验证单写者角色走 lumberjack，并且确实会轮转。
func TestInitShareRotates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)

	// 避免把 6MB 测试数据打到测试输出。
	oldStderr := os.Stderr
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	os.Stderr = devnull
	defer func() {
		os.Stderr = oldStderr
		_ = devnull.Close()
	}()

	closeFn := Init(RoleShare)
	defer closeFn()

	// 写入 6MB，越过 5MB 阈值（lumberjack 以 MB 为单位）。
	payload := strings.Repeat("x", 1<<20)
	for i := 0; i < 6; i++ {
		log.Println(payload)
	}

	logDir := filepath.Join(dir, "language_projects", "acp-title-proxy")
	// lumberjack 的压缩是异步 mill，轮询等待归档压缩完成且可读。
	var content string
	for i := 0; i < 300; i++ {
		archives, _ := filepath.Glob(filepath.Join(logDir, "acp-title-proxy-*.log.gz"))
		for _, name := range archives {
			if got, err := tryReadGz(name); err == nil && got != "" {
				content = got
				break
			}
		}
		if content != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if content == "" {
		entries, _ := os.ReadDir(logDir)
		for _, e := range entries {
			st, _ := e.Info()
			t.Logf("logdir entry: %s (%d bytes)", e.Name(), st.Size())
		}
		t.Fatalf("no readable lumberjack archive produced in %s", logDir)
	}
}

// TestRoleFileName 验证角色与文件名映射固定。
func TestRoleFileName(t *testing.T) {
	cases := map[Role]string{
		RoleClient: "acp-title-proxy-client.log",
		RoleShare:  "acp-title-proxy.log",
		RolePool:   "acp-title-proxy-pool.log",
	}
	for role, want := range cases {
		if got := role.fileName(); got != want {
			t.Errorf("role %d filename = %q, want %q", role, got, want)
		}
	}
}
