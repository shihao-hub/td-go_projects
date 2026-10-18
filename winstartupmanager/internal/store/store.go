package store

import (
	"os"
	"path/filepath"

	"winstartupmanager/internal/env"
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

func BackupDir() (string, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	var base string
	if localAppData != "" {
		base = filepath.Join(localAppData, "WinStartupManager", "backups")
	} else {
		dataDir, err := DataDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(dataDir, "backups")
	}

	if err := os.MkdirAll(base, 0755); err != nil {
		return "", err
	}
	return base, nil
}