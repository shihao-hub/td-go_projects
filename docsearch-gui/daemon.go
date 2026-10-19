package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type ServerStatus struct {
	TotalFiles  int    `json:"total_files"`
	TotalChunks int    `json:"total_chunks"`
	ModelName   string `json:"model_name"`
	DbDir       string `json:"db_dir"`
}

type DaemonManager struct {
	serverURL string
	cmd       *exec.Cmd
	started   bool
}

func NewDaemonManager(url string) *DaemonManager {
	if url == "" {
		url = "http://127.0.0.1:8765"
	}
	return &DaemonManager{serverURL: url}
}

// IsAlive 检查 docsearch 服务端接口是否正常响应
func (d *DaemonManager) IsAlive() bool {
	client := http.Client{Timeout: 600 * time.Millisecond}
	resp, err := client.Get(d.serverURL + "/api/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// GetStatus 获取当前系统索引统计数据
func (d *DaemonManager) GetStatus() (*ServerStatus, error) {
	client := http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(d.serverURL + "/api/status")
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

// ReindexAll 触发增量/全量重新构建索引
func (d *DaemonManager) ReindexAll(force bool) error {
	client := http.Client{Timeout: 60 * time.Second}
	url := fmt.Sprintf("%s/api/reindex?force=%t", d.serverURL, force)
	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reindex failed with status %d", resp.StatusCode)
	}
	return nil
}

// locateDocsearchDir 智能探测 docsearch 项目目录
func locateDocsearchDir() string {
	if p := os.Getenv("DOCSEARCH_DIR"); p != "" {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
	}

	// 1. 尝试当前可执行文件相对路径
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates := []string{
			filepath.Join(exeDir, "..", "..", "python_projects", "docsearch"),
			filepath.Join(exeDir, "..", "python_projects", "docsearch"),
			filepath.Join(exeDir, "python_projects", "docsearch"),
		}
		for _, c := range candidates {
			clean := filepath.Clean(c)
			if fi, err := os.Stat(clean); err == nil && fi.IsDir() {
				return clean
			}
		}
	}

	// 2. 默认固定工程位置
	fallback := `D:\Users\language_projects\python_projects\docsearch`
	if fi, err := os.Stat(fallback); err == nil && fi.IsDir() {
		return fallback
	}

	return ""
}

// EnsureRunning 确保 docsearch 后台服务正在运行，若未运行则拉起
func (d *DaemonManager) EnsureRunning() error {
	if d.IsAlive() {
		return nil
	}

	docDir := locateDocsearchDir()
	if docDir == "" {
		return fmt.Errorf("未能定位 docsearch 项目目录，请检查路径或配置 DOCSEARCH_DIR")
	}

	uvPath, err := exec.LookPath("uv")
	if err != nil {
		uvPath = "uv"
	}

	// 使用 CREATE_NO_WINDOW 隐藏运行，杜绝终端黑框闪烁
	cmd := exec.Command(uvPath, "run", "--directory", docDir, "docsearch", "serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 docsearch 后台守护进程失败: %w", err)
	}

	d.cmd = cmd
	d.started = true

	// 等待端口探活就绪（最多等待 15 秒）
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待 docsearch 服务启动超时 (15s)")
		case <-time.After(300 * time.Millisecond):
			if d.IsAlive() {
				return nil
			}
		}
	}
}

// Stop 停止由壳子拉起的后台进程
func (d *DaemonManager) Stop() {
	if d.started && d.cmd != nil && d.cmd.Process != nil {
		// Windows 树杀子进程
		killCmd := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", d.cmd.Process.Pid))
		killCmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000,
		}
		_ = killCmd.Run()
		d.started = false
		d.cmd = nil
	}
}
