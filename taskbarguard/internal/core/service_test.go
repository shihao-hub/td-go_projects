package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testService(t *testing.T, root, configPath string) *Service {
	t.Helper()
	return New(Paths{Channel: "dev", ScriptsDir: root, ConfigPath: configPath})
}

func writeAppDir(t *testing.T, root, name string, styles ...string) {
	t.Helper()
	appDir := filepath.Join(root, name)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "patch_"+name+"_icon.py"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if len(styles) == 0 {
		return
	}
	iconDir := filepath.Join(appDir, "custom_icons")
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, style := range styles {
		if err := os.WriteFile(filepath.Join(iconDir, style+".ico"), []byte{}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunUsesMockRunner(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	writeAppDir(t, root, "vscode", "blue")

	service := testService(t, root, configPath)
	var seen []string
	service.Runner = func(name string, args ...string) (string, error) {
		seen = args
		return "ok", nil
	}

	if _, _, err := service.Run("vscode", RunOptions{Style: "blue"}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 || seen[0] != "run" || seen[2] != "--custom-icon" || seen[3] != filepath.Join(root, "vscode", "custom_icons", "blue.ico") {
		t.Fatalf("unexpected args: %#v", seen)
	}
}

func TestRunPassesKillFlag(t *testing.T) {
	root := t.TempDir()
	writeAppDir(t, root, "vscode", "blue")
	service := testService(t, root, filepath.Join(root, "config.json"))
	var seen []string
	service.Runner = func(name string, args ...string) (string, error) {
		seen = args
		return "", nil
	}

	if _, _, err := service.Run("vscode", RunOptions{Style: "blue", Kill: true}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 5 || seen[4] != "--kill" {
		t.Fatalf("unexpected args: %#v", seen)
	}
}

func TestRunPassesRestartExplorer(t *testing.T) {
	root := t.TempDir()
	writeAppDir(t, root, "vscode", "blue")
	service := testService(t, root, filepath.Join(root, "config.json"))
	var seen []string
	service.Runner = func(name string, args ...string) (string, error) {
		seen = args
		return "", nil
	}

	if _, _, err := service.Run("vscode", RunOptions{Style: "blue", RestartExplorer: true}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 5 || seen[4] != "--restart-explorer" {
		t.Fatalf("unexpected args: %#v", seen)
	}
}

func TestRunFallsBackToConfigStyle(t *testing.T) {
	root := t.TempDir()
	writeAppDir(t, root, "vscode", "silver")
	configPath := filepath.Join(root, "config.json")
	if err := SaveConfig(configPath, Config{Apps: map[string]AppConfig{
		"vscode": {Enabled: true, Style: "silver"},
	}}); err != nil {
		t.Fatal(err)
	}
	service := testService(t, root, configPath)
	var seen []string
	service.Runner = func(name string, args ...string) (string, error) {
		seen = args
		return "", nil
	}

	if _, _, err := service.Run("vscode", RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 || seen[2] != "--custom-icon" || seen[3] != filepath.Join(root, "vscode", "custom_icons", "silver.ico") {
		t.Fatalf("unexpected args: %#v", seen)
	}
}

func TestRunRejectsUnknownStyle(t *testing.T) {
	root := t.TempDir()
	writeAppDir(t, root, "vscode", "blue")
	service := testService(t, root, filepath.Join(root, "config.json"))
	service.Runner = func(name string, args ...string) (string, error) {
		t.Fatal("runner must not be called for unknown style")
		return "", nil
	}

	_, _, err := service.Run("vscode", RunOptions{Style: "missing"})
	if err == nil || ErrorCode(err) != "style_not_found" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRevertUsesMockRunner(t *testing.T) {
	for _, name := range []string{"wt", "vscode"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeAppDir(t, root, name)
			service := testService(t, root, filepath.Join(root, "config.json"))
			called := false
			service.Runner = func(command string, args ...string) (string, error) {
				called = true
				if command != "uv" || len(args) != 3 || args[0] != "run" || args[2] != "--revert" {
					t.Fatalf("unexpected mocked command: %s %#v", command, args)
				}
				return "", nil
			}

			if _, _, err := service.Revert(name, RevertOptions{}); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("expected runner to be called")
			}
		})
	}
}

func TestRevertPassesRestartExplorer(t *testing.T) {
	root := t.TempDir()
	writeAppDir(t, root, "vscode")
	service := testService(t, root, filepath.Join(root, "config.json"))
	var seen []string
	service.Runner = func(name string, args ...string) (string, error) {
		seen = args
		return "", nil
	}

	if _, _, err := service.Revert("vscode", RevertOptions{RestartExplorer: true}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 || seen[2] != "--revert" || seen[3] != "--restart-explorer" {
		t.Fatalf("unexpected args: %#v", seen)
	}
}

func TestApplyPassesKillFlag(t *testing.T) {
	root := t.TempDir()
	writeAppDir(t, root, "vscode", "silver")
	configPath := filepath.Join(root, "config.json")
	if err := SaveConfig(configPath, Config{Apps: map[string]AppConfig{
		"vscode": {Enabled: true, Style: "silver"},
	}}); err != nil {
		t.Fatal(err)
	}
	service := testService(t, root, configPath)
	var seen []string
	service.Runner = func(name string, args ...string) (string, error) {
		seen = args
		return "", nil
	}

	applied, _, err := service.Apply(ApplyOptions{Kill: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != "vscode" {
		t.Fatalf("unexpected applied: %#v", applied)
	}
	if len(seen) != 5 || seen[2] != "--custom-icon" || seen[4] != "--kill" {
		t.Fatalf("unexpected args: %#v", seen)
	}
}

func TestApplyRunsEnabledAppsOnly(t *testing.T) {
	root := t.TempDir()
	writeAppDir(t, root, "vscode", "blue")
	writeAppDir(t, root, "deepseek_harness")
	configPath := filepath.Join(root, "config.json")
	if err := SaveConfig(configPath, Config{Apps: map[string]AppConfig{
		"vscode":           {Enabled: true, Style: "blue"},
		"deepseek_harness": {Enabled: false, Style: "ring"},
	}}); err != nil {
		t.Fatal(err)
	}
	service := testService(t, root, configPath)
	var calls int
	service.Runner = func(name string, args ...string) (string, error) {
		calls++
		return "", nil
	}

	applied, _, err := service.Apply(ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one runner call, got %d", calls)
	}
	if len(applied) != 1 || applied[0] != "vscode" {
		t.Fatalf("unexpected applied: %#v", applied)
	}
}

func TestApplySkipsDisabledExplicitApp(t *testing.T) {
	root := t.TempDir()
	writeAppDir(t, root, "vscode", "blue")
	configPath := filepath.Join(root, "config.json")
	if err := SaveConfig(configPath, Config{Apps: map[string]AppConfig{
		"vscode": {Enabled: false, Style: "blue"},
	}}); err != nil {
		t.Fatal(err)
	}
	service := testService(t, root, configPath)
	service.Runner = func(name string, args ...string) (string, error) {
		t.Fatal("runner must not be called for disabled app")
		return "", nil
	}

	applied, _, err := service.Apply(ApplyOptions{App: "vscode"})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Fatalf("unexpected applied: %#v", applied)
	}
}

func TestInstallTaskCallsSchtasks(t *testing.T) {
	service := testService(t, t.TempDir(), filepath.Join(t.TempDir(), "config.json"))
	var seen []string
	service.Runner = func(name string, args ...string) (string, error) {
		seen = append([]string{name}, args...)
		return "", nil
	}

	if _, err := service.InstallTask(); err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 || seen[0] != "schtasks" {
		t.Fatalf("unexpected command: %#v", seen)
	}
	joined := strings.Join(seen, " ")
	if !strings.Contains(joined, ScheduledTask) || !strings.Contains(joined, " apply") {
		t.Fatalf("unexpected task arguments: %#v", seen)
	}
}
