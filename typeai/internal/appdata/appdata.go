// Package appdata 解析 typeai 的运行时数据目录，并在首次进入通道目录时
// 把改造前位于 typeai 根目录的配置与会话复制过来（只复制，不删除旧文件）。
package appdata

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"typeai/internal/buildinfo"
)

const appName = "typeai"

// Root 返回通道目录的父目录（即改造前的旧数据根）。
func Root() (string, error) {
	if base := firstNonEmpty(os.Getenv("APPDATA"), os.Getenv("AppData")); base != "" {
		return filepath.Join(base, "language_projects", appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("定位数据目录失败: %w", err)
	}
	return filepath.Join(home, ".language_projects", appName), nil
}

// Dir 返回当前通道的数据目录并确保其存在。
// 通道目录首次创建时从旧根迁移 config.json 与 sessions/。
func Dir() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, buildinfo.Channel())

	_, statErr := os.Stat(dir)
	fresh := os.IsNotExist(statErr)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建数据目录失败: %w", err)
	}
	if fresh {
		migrateLegacy(root, dir)
	}
	return dir, nil
}

// migrateLegacy 把旧根下的配置与会话复制到新通道目录；目标已存在时跳过。
func migrateLegacy(root, dir string) {
	copyFileIfAbsent(filepath.Join(root, "config.json"), filepath.Join(dir, "config.json"))

	srcSessions := filepath.Join(root, "sessions")
	entries, err := os.ReadDir(srcSessions)
	if err != nil {
		return
	}
	dstSessions := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(dstSessions, 0o755); err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		copyFileIfAbsent(filepath.Join(srcSessions, entry.Name()), filepath.Join(dstSessions, entry.Name()))
	}
}

func copyFileIfAbsent(src, dst string) {
	if _, err := os.Stat(dst); err == nil {
		return
	}
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return
	}
	_ = out.Close()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
