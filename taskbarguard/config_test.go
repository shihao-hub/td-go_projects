package main

import (
	"bytes"
	"encoding/json"
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
	if !config.Apps["vscode"].Enabled || config.Apps["vscode"].Style != "blue" {
		t.Fatalf("unexpected defaults: %#v", config.Apps)
	}
	if !config.Apps["idea"].Enabled || config.Apps["idea"].Style != "brand" {
		t.Fatalf("unexpected idea defaults: %#v", config.Apps)
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

func TestSetConfigUpdatesPreference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if code := run([]string{"set", "--config", path, "vscode", "--style", "silver"}, discardWriter{}, discardWriter{}); code != 0 {
		t.Fatalf("set returned %d", code)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Apps["vscode"].Style != "silver" || !config.Apps["vscode"].Enabled {
		t.Fatalf("unexpected config after set: %#v", config.Apps["vscode"])
	}
}

func TestSetConfigRejectsEmptyUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var stderr bytes.Buffer
	if code := run([]string{"set", "--config", path, "vscode"}, discardWriter{}, &stderr); code == 0 {
		t.Fatal("expected set without options to fail")
	}
}

func TestRunPatchFallsBackToConfigStyle(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "vscode")
	if err := os.Mkdir(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "patch_vscode_icon.py"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "vscode_dark_silver.ico"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	if err := SaveConfig(configPath, Config{Apps: map[string]AppConfig{"vscode": {Enabled: true, Style: "silver"}}}); err != nil {
		t.Fatal(err)
	}

	var seen []string
	original := commandRunner
	commandRunner = func(name string, args ...string) error {
		seen = args
		return nil
	}
	t.Cleanup(func() { commandRunner = original })

	if code := run([]string{"run", "--config", configPath, "--scripts-dir", root, "vscode"}, discardWriter{}, discardWriter{}); code != 0 {
		t.Fatalf("run returned %d", code)
	}
	if len(seen) != 4 || seen[0] != "run" || seen[2] != "--style" || seen[3] != "silver" {
		t.Fatalf("unexpected args: %#v", seen)
	}
}

func TestApplyRunsEnabledAppsOnly(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"vscode", "deepseek_harness"} {
		appDir := filepath.Join(root, name)
		if err := os.Mkdir(appDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(appDir, "patch_icon.py"), []byte{}, 0o600); err != nil {
			t.Fatal(err)
		}
		if name == "vscode" {
			if err := os.WriteFile(filepath.Join(appDir, "vscode_dark_blue.ico"), []byte{}, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	configPath := filepath.Join(root, "config.json")
	if err := SaveConfig(configPath, Config{Apps: map[string]AppConfig{
		"vscode":           {Enabled: true, Style: "blue"},
		"deepseek_harness": {Enabled: false, Style: "ring"},
	}}); err != nil {
		t.Fatal(err)
	}

	var applied []string
	original := commandRunner
	commandRunner = func(name string, args ...string) error {
		applied = append(applied, args[len(args)-1])
		return nil
	}
	t.Cleanup(func() { commandRunner = original })

	var stdout bytes.Buffer
	if code := run([]string{"apply", "--config", configPath, "--scripts-dir", root, "--json"}, &stdout, discardWriter{}); code != 0 {
		t.Fatalf("apply returned %d", code)
	}
	if len(applied) != 1 || applied[0] != "blue" {
		t.Fatalf("unexpected applied command count/args: %#v", applied)
	}
	var payload struct {
		Data struct {
			Applied []string `json:"applied"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Applied) != 1 || payload.Data.Applied[0] != "vscode" {
		t.Fatalf("unexpected json payload: %s", stdout.String())
	}
}

func TestShowConfigPrintsJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var stdout bytes.Buffer
	if code := run([]string{"config", "--file", path}, &stdout, discardWriter{}); code != 0 {
		t.Fatalf("config returned %d", code)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"apps"`)) {
		t.Fatalf("unexpected config output: %s", stdout.String())
	}
}
