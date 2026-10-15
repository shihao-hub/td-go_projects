package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverApps(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "vscode")
	if err := os.Mkdir(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"patch_vscode_icon.py", "vscode_dark_blue.ico", "vscode_dark_silver.ico"} {
		if err := os.WriteFile(filepath.Join(appDir, name), []byte{}, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	apps, err := discoverApps(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].Name != "vscode" || apps[0].Script == "" {
		t.Fatalf("unexpected apps: %#v", apps)
	}
	if !contains(apps[0].Styles, "blue") || !contains(apps[0].Styles, "silver") {
		t.Fatalf("unexpected styles: %#v", apps[0].Styles)
	}
}

func TestCommandSchemaDoesNotReadScripts(t *testing.T) {
	var stdout bytes.Buffer
	if code := run([]string{"list", "--schema", "--scripts-dir", filepath.Join(t.TempDir(), "missing")}, &stdout, discardWriter{}); code != 0 {
		t.Fatalf("list schema returned %d", code)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"name":"list"`)) {
		t.Fatalf("unexpected schema: %s", stdout.String())
	}
}

func TestRunPatchUsesMockRunner(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "vscode")
	if err := os.Mkdir(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(appDir, "patch_vscode_icon.py")
	if err := os.WriteFile(script, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "vscode_dark_blue.ico"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	called := false
	original := commandRunner
	commandRunner = func(name string, args ...string) error {
		called = true
		if name != "uv" || len(args) != 4 || args[0] != "run" || args[2] != "--style" || args[3] != "blue" {
			t.Fatalf("unexpected mocked command: %s %#v", name, args)
		}
		return nil
	}
	t.Cleanup(func() { commandRunner = original })

	if code := run([]string{"run", "--style", "blue", "--scripts-dir", root, "vscode"}, discardWriter{}, discardWriter{}); code != 0 {
		t.Fatalf("run returned %d", code)
	}
	if !called {
		t.Fatal("expected mocked command runner to be called")
	}
}

func TestRunPatchPassesKillFlag(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "vscode")
	if err := os.Mkdir(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "patch_vscode_icon.py"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "vscode_dark_blue.ico"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	called := false
	original := commandRunner
	commandRunner = func(name string, args ...string) error {
		called = true
		if name != "uv" || len(args) != 5 || args[0] != "run" || args[2] != "--style" || args[3] != "blue" || args[4] != "--kill" {
			t.Fatalf("unexpected mocked command: %s %#v", name, args)
		}
		return nil
	}
	t.Cleanup(func() { commandRunner = original })

	if code := run([]string{"run", "--kill", "--style", "blue", "--scripts-dir", root, "vscode"}, discardWriter{}, discardWriter{}); code != 0 {
		t.Fatalf("run returned %d", code)
	}
	if !called {
		t.Fatal("expected mocked command runner to be called")
	}
}

func TestFindAppAlias(t *testing.T) {
	root := t.TempDir()
	wtDir := filepath.Join(root, "wt")
	if err := os.Mkdir(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "patch_wt_icon.py"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	wtIcons := filepath.Join(wtDir, "custom_icons")
	if err := os.Mkdir(wtIcons, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtIcons, "underlit_chrome.ico"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, alias := range []string{"wt", "terminal", "wt.exe", "windowsterminal"} {
		app, err := findApp(root, alias)
		if err != nil {
			t.Fatalf("alias %q failed: %v", alias, err)
		}
		if app.Name != "wt" {
			t.Fatalf("expected wt, got %q", app.Name)
		}
		if !contains(app.Styles, "underlit_chrome") {
			t.Fatalf("expected style underlit_chrome in %#v", app.Styles)
		}
	}
}

func TestRevertPatchUsesMockRunner(t *testing.T) {
	for _, appName := range []string{"wt", "vscode"} {
		t.Run(appName, func(t *testing.T) {
			root := t.TempDir()
			appDir := filepath.Join(root, appName)
			if err := os.Mkdir(appDir, 0o755); err != nil {
				t.Fatal(err)
			}
			script := filepath.Join(appDir, fmt.Sprintf("patch_%s_icon.py", appName))
			if err := os.WriteFile(script, []byte{}, 0o600); err != nil {
				t.Fatal(err)
			}

			called := false
			original := commandRunner
			commandRunner = func(name string, args ...string) error {
				called = true
				if name != "uv" || len(args) != 3 || args[0] != "run" || args[2] != "--revert" {
					t.Fatalf("unexpected mocked command: %s %#v", name, args)
				}
				return nil
			}
			t.Cleanup(func() { commandRunner = original })

			if code := run([]string{"revert", "--scripts-dir", root, appName}, discardWriter{}, discardWriter{}); code != 0 {
				t.Fatalf("revert returned %d", code)
			}
			if !called {
				t.Fatal("expected mocked command runner to be called")
			}
		})
	}
}

func TestApplyPassesKillFlag(t *testing.T) {
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
	if err := SaveConfig(configPath, Config{Apps: map[string]AppConfig{
		"vscode": {Enabled: true, Style: "silver"},
	}}); err != nil {
		t.Fatal(err)
	}

	var seen []string
	original := commandRunner
	commandRunner = func(name string, args ...string) error {
		seen = args
		return nil
	}
	t.Cleanup(func() { commandRunner = original })

	if code := run([]string{"apply", "--kill", "--config", configPath, "--scripts-dir", root}, discardWriter{}, discardWriter{}); code != 0 {
		t.Fatalf("apply returned %d", code)
	}
	if len(seen) != 5 || seen[0] != "run" || seen[2] != "--style" || seen[3] != "silver" || seen[4] != "--kill" {
		t.Fatalf("unexpected args: %#v", seen)
	}
}

func TestRevertPatchPassesRestartExplorer(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "vscode")
	if err := os.Mkdir(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "patch_vscode_icon.py"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	var seen []string
	original := commandRunner
	commandRunner = func(name string, args ...string) error {
		seen = args
		return nil
	}
	t.Cleanup(func() { commandRunner = original })

	if code := run([]string{"revert", "--restart-explorer", "--scripts-dir", root, "vscode"}, discardWriter{}, discardWriter{}); code != 0 {
		t.Fatalf("revert returned %d", code)
	}
	if len(seen) != 4 || seen[0] != "run" || seen[2] != "--revert" || seen[3] != "--restart-explorer" {
		t.Fatalf("unexpected args: %#v", seen)
	}
}

func TestRunPatchPassesRestartExplorer(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "vscode")
	if err := os.Mkdir(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "patch_vscode_icon.py"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "vscode_dark_blue.ico"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	var seen []string
	original := commandRunner
	commandRunner = func(name string, args ...string) error {
		seen = args
		return nil
	}
	t.Cleanup(func() { commandRunner = original })

	if code := run([]string{"run", "--style", "blue", "--restart-explorer", "--scripts-dir", root, "vscode"}, discardWriter{}, discardWriter{}); code != 0 {
		t.Fatalf("run returned %d", code)
	}
	if len(seen) != 5 || seen[0] != "run" || seen[2] != "--style" || seen[3] != "blue" || seen[4] != "--restart-explorer" {
		t.Fatalf("unexpected args: %#v", seen)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
