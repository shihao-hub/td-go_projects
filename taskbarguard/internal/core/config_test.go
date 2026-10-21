package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigInitializesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Apps["vscode"].Enabled || config.Apps["vscode"].Style != "silver" {
		t.Fatalf("unexpected defaults: %#v", config.Apps)
	}
	if config.Apps["idea"].Enabled || config.Apps["idea"].Style != "underlit_chrome" {
		t.Fatalf("unexpected idea defaults: %#v", config.Apps)
	}
	if !config.Apps["wt"].Enabled || config.Apps["wt"].Style != "pure_dark" {
		t.Fatalf("unexpected wt defaults: %#v", config.Apps)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("default config not persisted: %v", err)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	config := Config{Apps: map[string]AppConfig{
		"vscode": {Enabled: false, CustomIcon: filepath.Join("custom_icons", "ring.ico")},
	}}
	if err := SaveConfig(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Apps["vscode"].Enabled || loaded.Apps["vscode"].Style != "" {
		t.Fatalf("unexpected loaded config: %#v", loaded.Apps)
	}
	if loaded.Apps["vscode"].CustomIcon != config.Apps["vscode"].CustomIcon {
		t.Fatalf("custom icon mismatch: %#v", loaded.Apps["vscode"])
	}
}

func TestSetPreferenceUpdatesPreference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	service := New(Paths{Channel: "dev", ScriptsDir: t.TempDir(), ConfigPath: path})
	if _, _, err := service.SetPreference("vscode", "silver", ""); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Apps["vscode"].Style != "silver" || config.Apps["vscode"].CustomIcon != "" {
		t.Fatalf("unexpected config after set: %#v", config.Apps["vscode"])
	}
}

func TestSetPreferenceRejectsEmptyUpdate(t *testing.T) {
	service := New(Paths{Channel: "dev", ScriptsDir: t.TempDir(), ConfigPath: filepath.Join(t.TempDir(), "config.json")})
	if _, _, err := service.SetPreference("vscode", "", ""); err == nil || ErrorCode(err) != "invalid_arguments" {
		t.Fatalf("expected invalid_arguments, got %v", err)
	}
}

func TestSetEnabledToggles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	service := New(Paths{Channel: "dev", ScriptsDir: t.TempDir(), ConfigPath: path})
	config, preference, err := service.SetEnabled("vscode", false)
	if err != nil {
		t.Fatal(err)
	}
	if preference.Enabled || config.Apps["vscode"].Enabled {
		t.Fatalf("expected disabled vscode: %#v", preference)
	}
	_, preference, err = service.SetEnabled("vscode", true)
	if err != nil {
		t.Fatal(err)
	}
	if !preference.Enabled {
		t.Fatalf("expected enabled vscode: %#v", preference)
	}
}
