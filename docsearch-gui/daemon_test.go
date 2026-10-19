package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
}

// newFakeProject 构造一个满足 isDocsearchProject 判定的最小项目目录。
func newFakeProject(t *testing.T, root string) string {
	t.Helper()
	proj := filepath.Join(root, "python_projects", "docsearch")
	mustWriteFile(t, filepath.Join(proj, "pyproject.toml"), "[project]\nname = \"docsearch\"\n")
	if err := os.MkdirAll(filepath.Join(proj, "src", "docsearch"), 0o755); err != nil {
		t.Fatalf("创建 src 目录失败: %v", err)
	}
	return proj
}

func TestFindDocsearchDirWalksUpHierarchy(t *testing.T) {
	root := t.TempDir()
	proj := newFakeProject(t, root)

	// 模拟 <repo>/go_projects/docsearch-gui/bin 下的可执行文件
	start := filepath.Join(root, "go_projects", "docsearch-gui", "bin")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatalf("创建 bin 目录失败: %v", err)
	}

	if got := findDocsearchDir(start, maxParentSearch); got != proj {
		t.Fatalf("findDocsearchDir = %q, want %q", got, proj)
	}
}

func TestFindDocsearchDirReturnsEmptyWhenMissing(t *testing.T) {
	root := t.TempDir()
	start := filepath.Join(root, "go_projects", "docsearch-gui", "bin")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if got := findDocsearchDir(start, maxParentSearch); got != "" {
		t.Fatalf("findDocsearchDir = %q, want empty", got)
	}
}

func TestIsDocsearchProjectRejectsPlainDir(t *testing.T) {
	if isDocsearchProject(t.TempDir()) {
		t.Fatal("空目录不应被识别为 docsearch 项目")
	}
	if isDocsearchProject("") {
		t.Fatal("空字符串不应被识别为 docsearch 项目")
	}
}

func TestLocateDocsearchDirPrefersEnv(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "my", "docsearch")
	mustWriteFile(t, filepath.Join(proj, "pyproject.toml"), "[project]\nname = \"docsearch\"\n")
	if err := os.MkdirAll(filepath.Join(proj, "src", "docsearch"), 0o755); err != nil {
		t.Fatalf("创建 src 目录失败: %v", err)
	}
	t.Setenv("DOCSEARCH_DIR", proj)

	got, err := locateDocsearchDir()
	if err != nil {
		t.Fatalf("locateDocsearchDir 返回错误: %v", err)
	}
	if got != filepath.Clean(proj) {
		t.Fatalf("locateDocsearchDir = %q, want %q", got, proj)
	}
}

func TestLocateDocsearchDirRejectsInvalidEnv(t *testing.T) {
	t.Setenv("DOCSEARCH_DIR", t.TempDir())

	if _, err := locateDocsearchDir(); err == nil {
		t.Fatal("非 docsearch 目录的 DOCSEARCH_DIR 应报错")
	}
}

func TestNewDaemonManagerBuildsURLAndClampsPort(t *testing.T) {
	if got := NewDaemonManager(9000).URL(); got != "http://127.0.0.1:9000" {
		t.Fatalf("URL = %q", got)
	}
	if got := NewDaemonManager(0).URL(); got != "http://127.0.0.1:8765" {
		t.Fatalf("非法端口应回退默认端口，实际 URL = %q", got)
	}
	if got := NewDaemonManager(70000).URL(); got != "http://127.0.0.1:8765" {
		t.Fatalf("越界端口应回退默认端口，实际 URL = %q", got)
	}
}

func TestEnsureRunningSkipsLaunchWhenServiceAlive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			t.Errorf("意外请求路径: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_files":1,"total_chunks":2,"model_name":"m","db_dir":"d"}`))
	}))
	defer srv.Close()

	d := NewDaemonManager(8765)
	d.serverURL = srv.URL

	if err := d.EnsureRunning(); err != nil {
		t.Fatalf("EnsureRunning 返回错误: %v", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started || d.cmd != nil {
		t.Fatal("服务已存活时不应再拉起本地进程")
	}
}

func TestGetStatusDecodesPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_files":3351,"total_chunks":54075,"model_name":"bge","db_dir":"x"}`))
	}))
	defer srv.Close()

	d := NewDaemonManager(8765)
	d.serverURL = srv.URL

	st, err := d.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus 返回错误: %v", err)
	}
	if st.TotalFiles != 3351 || st.TotalChunks != 54075 {
		t.Fatalf("解析结果异常: %+v", st)
	}
}

func TestGetStatusRejectsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := NewDaemonManager(8765)
	d.serverURL = srv.URL

	if _, err := d.GetStatus(); err == nil {
		t.Fatal("非 200 响应应返回错误")
	}
}

func TestIsAliveTrueOnlyOn200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	d := NewDaemonManager(8765)
	d.serverURL = srv.URL

	if d.IsAlive() {
		t.Fatal("404 响应不应被视为存活")
	}
}

func TestReindexAllSendsForceFlag(t *testing.T) {
	var gotPath, gotForce string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotForce = r.URL.Query().Get("force")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	d := NewDaemonManager(8765)
	d.serverURL = srv.URL

	if err := d.ReindexAll(false); err != nil {
		t.Fatalf("ReindexAll 返回错误: %v", err)
	}
	if gotPath != "/api/reindex" {
		t.Fatalf("请求路径 = %q", gotPath)
	}
	if gotForce != "false" {
		t.Fatalf("force 参数 = %q, want false", gotForce)
	}
}

func TestReindexAllSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"detail":"index corrupted"}`))
	}))
	defer srv.Close()

	d := NewDaemonManager(8765)
	d.serverURL = srv.URL

	err := d.ReindexAll(true)
	if err == nil {
		t.Fatal("HTTP 500 应返回错误")
	}
}

func TestEnsureRunningTimeoutIsFinite(t *testing.T) {
	// 锁定常量，避免有人把它调成无限等待拖死启动流程
	if serviceReadyWait <= 0 || serviceReadyWait > 2*time.Minute {
		t.Fatalf("serviceReadyWait 取值不合理: %s", serviceReadyWait)
	}
	if reindexTimeout < time.Minute {
		t.Fatalf("重建索引超时过短，会误判失败: %s", reindexTimeout)
	}
}
