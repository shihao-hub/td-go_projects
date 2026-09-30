package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FindProjectRoot 向上递归查找项目顶层根目录（包含 plans_status.sql 或顶层 .git 根目录）
func FindProjectRoot(startDir string) string {
	curr, err := filepath.Abs(startDir)
	if err != nil {
		return startDir
	}

	bestRoot := ""
	for {
		sqlFile := filepath.Join(curr, "plans_status.sql")
		gitDir := filepath.Join(curr, ".git")

		// 如果当前目录有 plans_status.sql，这是最权威的项目事实根
		if _, err := os.Stat(sqlFile); err == nil {
			return curr
		}

		// 检查是否存在真实 .git 目录（排除 submodule 的 .git 文件）
		if fi, err := os.Stat(gitDir); err == nil {
			if fi.IsDir() && bestRoot == "" {
				bestRoot = curr
			}
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}

	if bestRoot != "" {
		return bestRoot
	}
	return startDir
}

// GetDataDir 获取标准数据存放目录，严格遵守仓库规范：
// 优先 %APPDATA%\language_projects\plansql\，其次 ~/.language_projects/plansql/
func GetDataDir() (string, error) {
	appData := os.Getenv("APPDATA")
	var baseDir string
	if appData != "" {
		baseDir = filepath.Join(appData, "language_projects", "plansql")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home dir: %w", err)
		}
		baseDir = filepath.Join(home, ".language_projects", "plansql")
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("create data dir %s: %w", baseDir, err)
	}
	return baseDir, nil
}

// GetAddressFilePath 返回记录当前运行服务端口与地址的文件路径
func GetAddressFilePath() (string, error) {
	dir, err := GetDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "plansql.addr"), nil
}

// WriteAddressFile 写入服务监听地址
func WriteAddressFile(addr string) error {
	path, err := GetAddressFilePath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(addr), 0644)
}

// RemoveAddressFile 清除地址文件
func RemoveAddressFile() error {
	path, err := GetAddressFilePath()
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// ReadAddressFile 读取现有守护进程地址
func ReadAddressFile() (string, error) {
	path, err := GetAddressFilePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// IdleTracker 管理守护进程闲置超时优雅退出
type IdleTracker struct {
	timeout      time.Duration
	activeCount  int
	lastActivity time.Time
	timer        *time.Timer
	onTimeout    func()
}

// NewIdleTracker 创建闲置跟踪器
func NewIdleTracker(timeout time.Duration, onTimeout func()) *IdleTracker {
	it := &IdleTracker{
		timeout:      timeout,
		lastActivity: time.Now(),
		onTimeout:    onTimeout,
	}
	it.timer = time.AfterFunc(timeout, func() {
		if it.activeCount == 0 {
			it.onTimeout()
		}
	})
	return it
}

// BeginRequest 开始一个请求
func (it *IdleTracker) BeginRequest() {
	it.activeCount++
	it.timer.Stop()
}

// EndRequest 结束一个请求
func (it *IdleTracker) EndRequest() {
	it.activeCount--
	if it.activeCount <= 0 {
		it.activeCount = 0
		it.lastActivity = time.Now()
		it.timer.Reset(it.timeout)
	}
}

// ParsePortFromAddr 从 "127.0.0.1:18090" 提取端口
func ParsePortFromAddr(addr string) (int, error) {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return 0, fmt.Errorf("invalid addr: %s", addr)
	}
	return strconv.Atoi(addr[idx+1:])
}
