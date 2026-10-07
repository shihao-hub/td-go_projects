package terminal

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitExit 等待 OnExit 回调，超时视为失败。
func waitExit(t *testing.T, ch <-chan exitInfo) exitInfo {
	t.Helper()
	select {
	case info := <-ch:
		return info
	case <-time.After(15 * time.Second):
		t.Fatal("等待子进程退出超时")
		return exitInfo{}
	}
}

type exitInfo struct {
	code int
	err  error
}

// collectData 收集 OnData 输出（并发安全）。
type collectData struct {
	mu sync.Mutex
	sb strings.Builder
}

func (c *collectData) onChunk(b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sb.Write(b)
}

func (c *collectData) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sb.String()
}

// cmdPath 解析 cmd.exe 绝对路径（Start 只接受确切路径，PATH 解析属壳的定位层）。
func cmdPath(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Skipf("找不到 cmd.exe: %v", err)
	}
	return p
}

func TestStartExitCode(t *testing.T) {
	exits := make(chan exitInfo, 1)
	sess, err := Start(cmdPath(t), 80, 25)
	if err != nil {
		t.Fatalf("启动会话失败: %v", err)
	}
	defer sess.Close()
	sess.OnExit = func(code int, err error) { exits <- exitInfo{code, err} }

	// 启动一个立即以非零码退出的子进程
	if err := sess.Write([]byte("exit 7\r\n")); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	info := waitExit(t, exits)
	if info.code != 7 {
		t.Fatalf("期望退出码 7，实际 %d（err=%v）", info.code, info.err)
	}
}

func TestOutputForward(t *testing.T) {
	exits := make(chan exitInfo, 1)
	out := &collectData{}
	sess, err := Start(cmdPath(t), 80, 25)
	if err != nil {
		t.Fatalf("启动会话失败: %v", err)
	}
	defer sess.Close()
	sess.OnData = out.onChunk
	sess.OnExit = func(code int, err error) { exits <- exitInfo{code, err} }

	if err := sess.Write([]byte("echo terminal_bridge_ok\r\n")); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	// 交互式 cmd 执行完 echo 停在提示符，再发 exit 触发退出
	if err := sess.Write([]byte("exit\r\n")); err != nil {
		t.Fatalf("写入 exit 失败: %v", err)
	}
	info := waitExit(t, exits)
	if info.code != 0 {
		t.Fatalf("期望退出码 0，实际 %d（err=%v）", info.code, info.err)
	}
	if !strings.Contains(out.String(), "terminal_bridge_ok") {
		t.Fatalf("输出未包含期望标记，实际: %q", out.String())
	}
}

func TestResizeAndInteractiveEcho(t *testing.T) {
	out := &collectData{}
	sess, err := Start(cmdPath(t), 80, 25)
	if err != nil {
		t.Fatalf("启动会话失败: %v", err)
	}
	defer sess.Close()
	sess.OnData = out.onChunk

	if err := sess.Resize(100, 30); err != nil {
		t.Fatalf("调整尺寸失败: %v", err)
	}
	if err := sess.Resize(0, 30); err == nil {
		t.Fatal("非正尺寸应报错")
	}

	// 交互回显：cmd 交互提示符下输入文本应回显到 PTY
	if err := sess.Write([]byte("echo PTY_ECHO_MARK_42\r\n")); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "PTY_ECHO_MARK_42") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("交互回显超时，实际输出: %q", out.String())
}

func TestCloseIdempotentAndKill(t *testing.T) {
	sess, err := Start(cmdPath(t), 80, 25)
	if err != nil {
		t.Fatalf("启动会话失败: %v", err)
	}
	sess.Close()
	sess.Close() // 幂等：第二次不 panic
	if err := sess.Write([]byte("x")); err != ErrSessionClosed {
		t.Fatalf("关闭后写入应返回 ErrSessionClosed，实际 %v", err)
	}
	if err := sess.Resize(80, 25); err != ErrSessionClosed {
		t.Fatalf("关闭后 Resize 应返回 ErrSessionClosed，实际 %v", err)
	}
}

func TestStartInvalidArgs(t *testing.T) {
	if _, err := Start("", 80, 25); err == nil {
		t.Fatal("空路径应报错")
	}
	missing := filepath.Join(t.TempDir(), "no_such_typeai.exe")
	if _, err := Start(missing, 80, 25); err == nil {
		t.Fatal("不存在路径应报错")
	}
}
