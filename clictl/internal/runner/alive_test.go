package runner

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"clictl/internal/store"
)

// deadPID 起一个临时进程并等它退出，返回其 PID（必然已死）。
// 进程对象销毁与句柄释放同步，但仍留短暂重试兜底内核侧竞态；
// 注：写死的"高位 PID 必不存在"不可靠（实测 4194304 可能被真实进程占用）
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("cmd", "/c", "exit")
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动临时进程失败: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait()
	for i := 0; i < 50; i++ {
		if !Alive(pid, "") {
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("临时进程 %d 退出后探活未消失", pid)
	return 0
}

// 探活单测（表驱动 + 真实路径比对）
func TestAlive(t *testing.T) {
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("获取测试进程路径失败: %v", err)
	}
	dead := deadPID(t)

	cases := []struct {
		name   string
		pid    int
		path   string
		expect bool
	}{
		{"自身进程仅存在性", os.Getpid(), "", true},
		{"自身进程路径匹配（真实比对）", os.Getpid(), selfExe, true},
		{"自身进程路径不匹配（防复用判死）", os.Getpid(), `C:\Windows\notepad.exe`, false},
		{"已退出进程仅存在性", dead, "", false},
		{"已退出进程带路径", dead, selfExe, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Alive(c.pid, c.path); got != c.expect {
				t.Fatalf("Alive(%d, %q) = %v, 期望 %v", c.pid, c.path, got, c.expect)
			}
		})
	}
}

func TestFilterAlive(t *testing.T) {
	selfExe, _ := os.Executable()
	dead := deadPID(t)
	pid1 := os.Getpid()

	launches := []store.Launch{
		{ID: 1, PID: &pid1}, // 活：自身
		{ID: 2, PID: &dead}, // 死：已退出进程
		{ID: 3, PID: nil},   // 无 PID（run 记录），永不入选
	}
	got := FilterAlive(launches, selfExe)
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("FilterAlive 应只剩 ID=1（自身），实际: %+v", got)
	}

	// 期望路径不匹配时全部判死
	if got := FilterAlive(launches, `C:\other.exe`); len(got) != 0 {
		t.Fatalf("路径全不匹配时应为空，实际: %+v", got)
	}
}
