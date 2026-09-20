package cli

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestMain 隔离数据目录（AppData → 临时目录）并把上游指到进程内 httptest
// （service.Open 经 GLM_QUOTA_WATCH_API_BASE 注入），绝不触网、不碰真实配置。
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "gqw-cli-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("AppData", tmp)

	var pct atomic.Int64
	pct.Store(2)
	reset := time.Now().Add(4*time.Hour + 43*time.Minute).UnixMilli()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"code":200,"msg":"操作成功","success":true,"data":{"level":"max","limits":[`+
			`{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":%d,"nextResetTime":%d}]}}`,
			pct.Load(), reset)
	}))
	defer srv.Close()
	os.Setenv("GLM_QUOTA_WATCH_API_BASE", srv.URL)

	code := m.Run()
	srv.Close()
	os.Exit(code)
}

// capture 截获标准流（os.Stdout/os.Stderr 在调用时求值，可直接替换）。
// 读取必须在后台并发进行：输出超过 pipe 缓冲（如 schema 的 7KB+）时，
// 先写后读会让写端阻塞在 writeFile 上造成死锁。
func capture(t *testing.T, target **os.File, fn func()) string {
	t.Helper()
	old := *target
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	*target = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	*target = old
	return <-done
}

func captureStdout(t *testing.T, fn func()) string { return capture(t, &os.Stdout, fn) }
func captureStderr(t *testing.T, fn func()) string { return capture(t, &os.Stderr, fn) }

func TestRunExitCodes(t *testing.T) {
	// 参数/flag 错误 → 2
	if code := Run([]string{"token", "set"}); code != 2 {
		t.Fatalf("参数不足应退出码 2: %d", code)
	}
	if code := Run([]string{"status", "--nope"}); code != 2 {
		t.Fatalf("未知 flag 应退出码 2: %d", code)
	}

	// 业务错误 → 1，stderr 人读提示
	var errOut string
	var code int
	errOut = captureStderr(t, func() { code = Run([]string{"token", "set", "short"}) })
	if code != 1 {
		t.Fatalf("短 token 应退出码 1: %d", code)
	}
	if !strings.Contains(errOut, "错误") {
		t.Fatalf("人读错误应到 stderr: %q", errOut)
	}

	// 配置成功 → 0
	if code := Run([]string{"token", "set", strings.Repeat("x", 30)}); code != 0 {
		t.Fatalf("token set 应成功: %d", code)
	}

	// config set 非法值 → 1；合法 → 0
	if code := Run([]string{"config", "set", "interval", "bogus"}); code != 1 {
		t.Fatalf("非法 interval 应退出码 1: %d", code)
	}
	if code := Run([]string{"config", "set", "thresholds", "80,50,50"}); code != 0 {
		t.Fatalf("thresholds 应成功: %d", code)
	}

	// status 人读 → 0，stdout 含表格
	var stdOut string
	stdOut = captureStdout(t, func() { code = Run([]string{"status"}) })
	if code != 0 {
		t.Fatalf("status 应成功: %d", code)
	}
	if !strings.Contains(stdOut, "GLM 用量") || !strings.Contains(stdOut, "5h 窗口") {
		t.Fatalf("人读 status 输出不符: %q", stdOut)
	}

	// status --json → stdout 信封
	stdOut = captureStdout(t, func() { code = Run([]string{"--json", "status"}) })
	if code != 0 {
		t.Fatalf("status --json 应成功: %d", code)
	}
	if !strings.Contains(stdOut, `"ok": true`) || !strings.Contains(stdOut, `"windows"`) {
		t.Fatalf("JSON 信封不符: %q", stdOut)
	}

	// token show --json → 脱敏
	stdOut = captureStdout(t, func() { code = Run([]string{"--json", "token", "show"}) })
	if code != 0 {
		t.Fatalf("token show 应成功: %d", code)
	}
	if !strings.Contains(stdOut, `"has_token": true`) {
		t.Fatalf("JSON 配置不符: %q", stdOut)
	}
	if strings.Contains(stdOut, strings.Repeat("x", 30)) {
		t.Fatalf("token show 不应输出明文: %q", stdOut)
	}
}

func TestSchemaExport(t *testing.T) {
	var code int
	stdOut := captureStdout(t, func() { code = Run([]string{"schema"}) })
	if code != 0 {
		t.Fatalf("schema 应成功: %d", code)
	}
	for _, want := range []string{"gqw.status", "gqw.token.set", "gqw.token.show", "gqw.token.remove", "gqw.config.show", "gqw.config.set"} {
		if !strings.Contains(stdOut, want) {
			t.Fatalf("schema 导出缺工具 %s", want)
		}
	}
}

func TestRootVersion(t *testing.T) {
	var code int
	stdOut := captureStdout(t, func() { code = Run([]string{"--version"}) })
	if code != 0 || !strings.Contains(stdOut, "dev") {
		t.Fatalf("--version 输出不符: code=%d out=%q", code, stdOut)
	}
}
