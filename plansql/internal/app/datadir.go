package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"plansql/internal/daemon"
)

// 项目根记忆：GUI 是「按项目工作」的工具，双击启动时 cwd 不可靠，
// 因此把上次打开的项目根落在当前通道的数据目录，下次直接恢复。

const projectConfigFileName = "project.json"

type projectConfig struct {
	RootDir string `json:"rootDir"`
}

func projectConfigPath() string {
	dir := appDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, projectConfigFileName)
}

// loadLastProjectRoot 读取上次打开的项目根；文件缺失、损坏或目录已不存在时返回空串。
func loadLastProjectRoot() string {
	path := projectConfigPath()
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg projectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		logf("解析项目配置失败，忽略历史记录: %v", err)
		return ""
	}
	if cfg.RootDir == "" {
		return ""
	}
	if info, err := os.Stat(cfg.RootDir); err != nil || !info.IsDir() {
		return ""
	}
	return cfg.RootDir
}

// saveLastProjectRoot 持久化项目根；写入失败只记日志，不影响主流程。
func saveLastProjectRoot(rootDir string) {
	path := projectConfigPath()
	if path == "" || rootDir == "" {
		return
	}
	raw, err := json.MarshalIndent(projectConfig{RootDir: rootDir}, "", "  ")
	if err != nil {
		logf("序列化项目配置失败: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logf("创建项目配置目录失败: %v", err)
		return
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		logf("保存项目配置失败: %v", err)
	}
}

// ResolveInitialProjectRoot 返回 GUI 首次渲染使用的项目根，以及它是否来自历史记忆。
func ResolveInitialProjectRoot() (root string, remembered bool) {
	if saved := loadLastProjectRoot(); saved != "" {
		return saved, true
	}

	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		if exe, exeErr := os.Executable(); exeErr == nil {
			cwd = filepath.Dir(exe)
		} else {
			cwd = "."
		}
	}
	return daemon.FindProjectRoot(cwd), false
}
