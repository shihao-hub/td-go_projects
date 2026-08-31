package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type config struct {
	LastDir string `json:"last_dir"`
}

func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "zread-tray", "config.json"), nil
}

func loadConfig() config {
	p, err := configPath()
	if err != nil {
		return config{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return config{}
	}
	var c config
	if json.Unmarshal(data, &c) != nil {
		return config{}
	}
	return c
}

func saveConfig(c config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
