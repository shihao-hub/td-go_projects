package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"clictl/internal/store"
)

// TestConsoleCtlHelper 作为子进程运行 consoleCtlLocal，避免把测试主进程
// 从自己的控制台 detach 掉。父测试通过 CLICTL_CONSOLE_HELPER=1 触发。
func TestConsoleCtlHelper(t *testing.T) {
	if os.Getenv("CLICTL_CONSOLE_HELPER") == "" {
		t.Skip("仅作为 console helper 子进程运行")
	}
	pid, err := strconv.Atoi(os.Getenv("CLICTL_CONSOLE_PID"))
	if err != nil || pid <= 0 {
		fmt.Fprintln(os.Stderr, "非法 PID")
		os.Exit(2)
	}
	action := os.Getenv("CLICTL_CONSOLE_ACTION")
	visible, err := consoleCtlLocal(pid, action)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
	if action == "status" {
		if visible {
			fmt.Print("visible")
		} else {
			fmt.Print("hidden")
		}
	}
	os.Exit(0)
}

func consoleCtlChild(t *testing.T, pid int, action string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestConsoleCtlHelper")
	cmd.Env = append(os.Environ(),
		"CLICTL_CONSOLE_HELPER=1",
		fmt.Sprintf("CLICTL_CONSOLE_PID=%d", pid),
		"CLICTL_CONSOLE_ACTION="+action,
	)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// consoleCtlRetry 模拟产品层 SetConsoleVisible 的重试：刚启动的控制台
// 可能尚未就绪，AttachConsole 会短暂报 The handle is invalid。
func consoleCtlRetry(t *testing.T, pid int, action string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		out, err := consoleCtlChild(t, pid, action)
		if err == nil {
			return out
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("console helper %s 重试失败: %v", action, lastErr)
	return ""
}

func waitConsoleStatus(t *testing.T, pid int, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		out, err := consoleCtlChild(t, pid, "status")
		if err == nil {
			last = out
			if out == want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("控制台状态 = %q, 期望 %q", last, want)
}

func TestHiddenConsoleShowHide(t *testing.T) {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		t.Skip("无 SystemRoot")
	}
	pingPath := filepath.Join(systemRoot, "System32", "ping.exe")
	if _, err := os.Stat(pingPath); err != nil {
		t.Skipf("无 ping.exe: %v", err)
	}

	dir := t.TempDir()
	st, err := store.OpenAt(filepath.Join(dir, "clictl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddTool(store.AddSpec{
		Name: "hidden-demo",
		Source: &store.SourceSpec{
			Program: pingPath,
			Workdir: work,
			Args:    []string{"-n", "30", "127.0.0.1"},
			Type:    store.SourceTypeGUI,
		},
	}); err != nil {
		t.Fatal(err)
	}

	res, rerr := StartHiddenConsole(st, "hidden-demo", "", nil)
	if rerr != nil {
		t.Fatalf("隐藏控制台启动失败: %v", rerr)
	}
	defer func() { _, _ = Stop(st, "hidden-demo") }()

	if res.Mode != ModeHidden || res.Target != store.TargetSource {
		t.Fatalf("启动结果异常: %+v", res)
	}
	waitConsoleStatus(t, res.PID, "hidden")

	consoleCtlRetry(t, res.PID, "show")
	waitConsoleStatus(t, res.PID, "visible")

	consoleCtlRetry(t, res.PID, "hide")
	waitConsoleStatus(t, res.PID, "hidden")

	stopRes, rerr := Stop(st, "hidden-demo")
	if rerr != nil {
		t.Fatalf("停止失败: %v", rerr)
	}
	if len(stopRes.Killed) != 1 {
		t.Fatalf("应终止 1 个实例，实际 %+v", stopRes)
	}
}
