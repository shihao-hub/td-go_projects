package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesEnvironmentOverrides(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{"base_url":"http://disk","api_key":"disk-key","model":"disk-model"}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv(envBaseURL, "http://env")
	t.Setenv(envAPIKey, "env-key")
	t.Setenv(envModel, "env-model")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BaseURL != "http://env" || cfg.APIKey != "env-key" || cfg.Model != "env-model" {
		t.Errorf("Load() = %#v, want environment values", cfg)
	}

	disk, err := LoadDisk(dir)
	if err != nil {
		t.Fatalf("LoadDisk() error = %v", err)
	}
	if disk.BaseURL != "http://disk" || disk.APIKey != "disk-key" || disk.Model != "disk-model" {
		t.Errorf("LoadDisk() = %#v, want disk values", disk)
	}
}

func TestSaveValidatesAndPersists(t *testing.T) {
	dir := t.TempDir()
	err := Save(dir, Config{BaseURL: "ftp://invalid", Model: "m"})
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Save() error = %#v, want ValidationError", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "config.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("invalid config wrote file: %v", statErr)
	}

	valid := Config{BaseURL: "http://localhost/v1", APIKey: "key", Model: "model"}
	if err := Save(dir, valid); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := LoadDisk(dir)
	if err != nil {
		t.Fatalf("LoadDisk() error = %v", err)
	}
	if loaded != valid {
		t.Errorf("loaded = %#v, want %#v", loaded, valid)
	}
}
