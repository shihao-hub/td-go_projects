package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	probeTimeout      = 700 * time.Millisecond
	statusTimeout     = 2 * time.Second
	reindexTimeout    = 30 * time.Minute
	serviceReadyWait  = 40 * time.Second
	servicePollPeriod = 300 * time.Millisecond
	deadPollGrace     = 10 // 进程提前退出后的宽限轮询次数（约 3s）
	maxParentSearch   = 6
)

type ServerStatus struct {
	TotalFiles  int    `json:"total_files"`
	TotalChunks int    `json:"total_chunks"`
	ModelName   string `json:"model_name"`
	DbDir       string `json:"db_dir"`
}

// DaemonManager 负责探测、拉起与回收 docsearch Python 后台服务。
// 所有导出方法均可被任意 goroutine 并发调用。
type DaemonManager struct {
	port      int
	serverURL string

	client     *http.Client
	longClient *http.Client

	ensureMu sync.Mutex // 串行化启动流程，避免并发拉起多个进程
	mu       sync.Mutex // 保护 cmd / started
	cmd      *exec.Cmd
	started  bool
}

func NewDaemonManager(port int) *DaemonManager {
	if port <= 0 || port > 65535 {
		port = defaultPort
	}
	return &DaemonManager{
		port:       port,
		serverURL:  fmt.Sprintf("http://127.0.0.1:%d", port),
		client:     &http.Client{Timeout: statusTimeout},
		longClient: &http.Client{Timeout: reindexTimeout},
	}
}

// URL 返回本实例连接的后台服务地址。
func (d *DaemonManager) URL() string { return d.serverURL }

// IsAlive 检查 docsearch 服务端接口是否正常响应。
func (d *DaemonManager) IsAlive() bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.serverURL+"/api/status", nil)
	if err != nil {
		return false
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	// 必须读空并关闭响应体，否则连接无法复用、socket 只会不断累积。
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode == http.StatusOK
}

// GetStatus 获取当前系统索引统计数据。
func (d *DaemonManager) GetStatus() (*ServerStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.serverURL+"/api/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var st ServerStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, err
	}
	return &st, nil
}

// ReindexAll 触发增量/全量重新构建索引。
// 服务端该接口为同步阻塞接口，全量重建可能耗时数分钟，因此不能用短超时误判失败。
func (d *DaemonManager) ReindexAll(force bool) error {
	url := fmt.Sprintf("%s/api/reindex?force=%t", d.serverURL, force)
	resp, err := d.longClient.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			return fmt.Errorf("reindex failed with status %d", resp.StatusCode)
		}
		return fmt.Errorf("reindex failed with status %d: %s", resp.StatusCode, detail)
	}
	return nil
}

// EnsureRunning 确保 docsearch 后台服务正在运行，若未运行则拉起。
// 多入口（启动协程、托盘点击、第二实例唤醒）并发调用时只会拉起一个进程。
func (d *DaemonManager) EnsureRunning() error {
	d.ensureMu.Lock()
	defer d.ensureMu.Unlock()

	if d.IsAlive() {
		return nil
	}

	docDir, err := locateDocsearchDir()
	if err != nil {
		return err
	}

	uvPath, err := exec.LookPath("uv")
	if err != nil {
		return fmt.Errorf("未找到 uv 可执行文件，无法拉起 docsearch 服务: %w", err)
	}

	args := []string{"run", "--directory", docDir, "docsearch", "serve", "--port", strconv.Itoa(d.port)}
	cmd := newHiddenCommand(uvPath, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 docsearch 后台服务失败: %w", err)
	}
	logf("已拉起 docsearch 服务: dir=%s port=%d pid=%d", docDir, d.port, cmd.Process.Pid)

	d.mu.Lock()
	d.cmd = cmd
	d.started = true
	d.mu.Unlock()

	// 回收子进程句柄，并在进程提前退出时更新内部状态，避免 Stop 杀错已失效的 PID。
	go d.watchProcess(cmd)

	deadline := time.Now().Add(serviceReadyWait)
	deadPolls := 0
	for time.Now().Before(deadline) {
		if d.IsAlive() {
			return nil
		}
		// 我们拉起的进程已退出且端口仍不可达：快速失败，不再白等整个超时窗口。
		// 宽限若干次轮询，规避 uv 启动瞬间的进程状态竞态。
		if !d.tracks(cmd) {
			deadPolls++
			if deadPolls >= deadPollGrace {
				return fmt.Errorf("docsearch 服务进程提前退出（端口 %d 可能被占用或环境异常，详见日志）", d.port)
			}
		} else {
			deadPolls = 0
		}
		time.Sleep(servicePollPeriod)
	}
	return fmt.Errorf("等待 docsearch 服务就绪超时 (%s)", serviceReadyWait)
}

// tracks 判断给定命令是否仍是被本管理器托管、尚未退出的子进程。
func (d *DaemonManager) tracks(cmd *exec.Cmd) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.started && d.cmd == cmd
}

// watchProcess 等待子进程结束并回收句柄。
func (d *DaemonManager) watchProcess(cmd *exec.Cmd) {
	err := cmd.Wait()

	d.mu.Lock()
	stillOurs := d.cmd == cmd
	if stillOurs {
		d.cmd = nil
		d.started = false
	}
	d.mu.Unlock()

	if stillOurs {
		logf("docsearch 服务进程退出: %v", err)
	}
}

// Stop 停止由本壳拉起且仍在运行的后台进程树。
func (d *DaemonManager) Stop() {
	d.mu.Lock()
	cmd := d.cmd
	started := d.started
	d.cmd = nil
	d.started = false
	d.mu.Unlock()

	if !started || cmd == nil || cmd.Process == nil {
		return
	}
	logf("正在回收 docsearch 服务进程树 pid=%d", cmd.Process.Pid)
	killTree(cmd.Process.Pid)
}

// isDocsearchProject 校验目录是否为真正的 docsearch 项目（防止误启动无关目录）。
func isDocsearchProject(dir string) bool {
	if dir == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, "pyproject.toml"))
	if err != nil || fi.IsDir() {
		return false
	}
	fi, err = os.Stat(filepath.Join(dir, "src", "docsearch"))
	return err == nil && fi.IsDir()
}

// findDocsearchDir 自 start 目录逐级上溯，寻找 <anywhere>/python_projects/docsearch。
func findDocsearchDir(start string, maxUp int) string {
	dir := filepath.Clean(start)
	for i := 0; i <= maxUp; i++ {
		candidate := filepath.Join(dir, "python_projects", "docsearch")
		if isDocsearchProject(candidate) {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// locateDocsearchDir 智能探测 docsearch 项目目录：环境变量 → 可执行文件上溯 → 工作目录上溯。
func locateDocsearchDir() (string, error) {
	if p := strings.TrimSpace(os.Getenv("DOCSEARCH_DIR")); p != "" {
		if isDocsearchProject(p) {
			return filepath.Clean(p), nil
		}
		return "", fmt.Errorf("DOCSEARCH_DIR=%s 不是有效的 docsearch 项目目录（缺少 pyproject.toml 或 src/docsearch）", p)
	}

	var starts []string
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	for _, start := range starts {
		if dir := findDocsearchDir(start, maxParentSearch); dir != "" {
			return dir, nil
		}
	}
	return "", fmt.Errorf("未能定位 docsearch 项目目录：请设置 DOCSEARCH_DIR，或将本程序放在 <仓库根>/go_projects/docsearch-gui/bin 下")
}
