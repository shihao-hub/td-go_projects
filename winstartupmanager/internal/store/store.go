package store

import (
	"io"
	"os"
	"path/filepath"

	"winstartupmanager/internal/env"
	"winstartupmanager/internal/logx"
)

func DataDir() (string, error) {
	appData := os.Getenv("APPDATA")
	var base string
	if appData != "" {
		base = filepath.Join(appData, "language_projects", "winstartupmanager")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".language_projects", "winstartupmanager")
	}

	mode := "prod"
	if env.IsDev() {
		mode = "dev"
	}
	dir := filepath.Join(base, mode)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// BackupDir 返回当前通道的备份目录：<DataDir>/backups。
func BackupDir() (string, error) {
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	migrateLegacyBackups(dir)
	return dir, nil
}

// legacyBackupDir 是历史版本使用的备份目录，仅用于一次性复制迁移。
func legacyBackupDir() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return ""
	}
	return filepath.Join(localAppData, "WinStartupManager", "backups")
}

// migrateLegacyBackups 把旧目录中的备份复制到新目录；源文件保留不删，已存在的同名文件不覆盖。
func migrateLegacyBackups(destDir string) {
	legacy := legacyBackupDir()
	if legacy == "" || legacy == destDir {
		return
	}
	entries, err := os.ReadDir(legacy)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		src := filepath.Join(legacy, entry.Name())
		dst := filepath.Join(destDir, entry.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := copyFile(src, dst); err != nil {
			logx.Warnf("迁移旧备份失败 file=%s: %v", entry.Name(), err)
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}
