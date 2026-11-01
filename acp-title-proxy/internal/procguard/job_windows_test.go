//go:build windows

package procguard

import (
	"os/exec"
	"testing"
	"time"
)

// TestJobKillsTreeOnClose 验证关闭 Job 句柄时，Job 内的进程树会被内核连带终止。
// 这是根治 AGY harness 泄漏的核心行为：宿主进程退出 -> Job 关闭 -> 整棵树收尸。
func TestJobKillsTreeOnClose(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "ping -n 60 127.0.0.1 > nul")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}

	guard, err := Attach(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("attach job object: %v", err)
	}

	// 给 cmd.exe 留出启动 ping 子进程的时间
	time.Sleep(1500 * time.Millisecond)

	if err := guard.Close(); err != nil {
		t.Fatalf("close job: %v", err)
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	select {
	case <-waitCh:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("process tree did not exit within 5s after job close")
	}
}
