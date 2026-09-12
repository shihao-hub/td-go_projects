//go:build !windows

package browse

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// lookZread 定位 zread 命令（npm 全局安装）
func lookZread() (string, error) {
	return exec.LookPath("zread")
}

// spawnDetached 后台启动（非 Windows：默认脱离会话语义由 nohup/systemd 场景保证，
// 这里直接 Start 后返回，父进程退出不回收子进程）
func spawnDetached(zreadCmd string, opt Options) (int, error) {
	logPath := filepath.Join(os.TempDir(), "zreadmanager.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("打开日志文件失败: %w", err)
	}
	defer logFile.Close()

	args := []string{"browse"}
	if opt.Host != "" {
		args = append(args, "--host", opt.Host)
	}
	if opt.Port > 0 {
		args = append(args, "--port", strconv.Itoa(opt.Port))
	}
	if opt.Generate {
		args = append(args, "--generate")
	}

	cmd := exec.Command(zreadCmd, args...)
	cmd.Dir = opt.Dir
	cmd.Stdout, cmd.Stderr = logFile, logFile

	fmt.Fprintf(logFile, "\n===== %s zreadmanager 启动 zread browse (dir=%s) =====\n",
		time.Now().Format("2006-01-02 15:04:05"), opt.Dir)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	// 防 zombie：不 Wait，父进程（CLI）退出后由 init 接管
	return cmd.Process.Pid, nil
}

// alive 信号 0 探活（创建时间比对在非 Windows 不可用，忽略）
func alive(pid int, createUnix int64) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// processCreateUnix 非 Windows 简化：返回 0 表示跳过比对
func processCreateUnix(pid int) (int64, error) {
	return 0, nil
}

// killTree 直接 Kill（不含子进程树）
func killTree(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
