package main

import (
	"bytes"
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

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
