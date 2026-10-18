package engine

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// resolveExePath 展开环境变量并解析可执行文件的真实路径与存在性。
// 裸文件名（不含目录分隔符）在按给定路径探测失败后，会依次尝试
// System32 与 PATH 目录，以还原 CreateProcess 对裸命令名的查找语义。
// 返回解析后的路径（即使不存在也返回展开后的原始路径）与存在性判定。
func resolveExePath(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}

	expanded := ExpandEnvStrings(trimmed)
	if expanded == "" {
		return "", false
	}

	if _, err := os.Stat(expanded); err == nil {
		return expanded, true
	}

	// 已含目录分隔符（绝对或相对路径）时不再做 System32/PATH 探测
	if strings.ContainsAny(expanded, `\/`) {
		return expanded, false
	}

	for _, dir := range bareNameSearchDirs() {
		cand := filepath.Join(dir, expanded)
		if _, err := os.Stat(cand); err == nil {
			return cand, true
		}
	}

	return expanded, false
}

// bareNameSearchDirs 返回裸文件名探测目录：System32 优先，其次为 PATH 中的各目录。
func bareNameSearchDirs() []string {
	var dirs []string
	if sysDir, err := windows.GetSystemDirectory(); err == nil {
		dirs = append(dirs, sysDir)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}
