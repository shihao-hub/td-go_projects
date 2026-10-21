package core

import (
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

	apps, err := DiscoverApps(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].Name != "vscode" || apps[0].Script == "" {
		t.Fatalf("unexpected apps: %#v", apps)
	}
	if !Contains(apps[0].Styles, "blue") || !Contains(apps[0].Styles, "silver") {
		t.Fatalf("unexpected styles: %#v", apps[0].Styles)
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
		app, err := FindApp(root, alias)
		if err != nil {
			t.Fatalf("alias %q failed: %v", alias, err)
		}
		if app.Name != "wt" {
			t.Fatalf("expected wt, got %q", app.Name)
		}
		if !Contains(app.Styles, "underlit_chrome") {
			t.Fatalf("expected style underlit_chrome in %#v", app.Styles)
		}
	}
}

func TestResolvePathsPrefersExplicitOverrides(t *testing.T) {
	paths := ResolvePaths("dev", "custom-scripts", "custom-config.json")
	if paths.ScriptsDir != "custom-scripts" {
		t.Fatalf("unexpected scripts dir: %s", paths.ScriptsDir)
	}
	if paths.ConfigPath != "custom-config.json" {
		t.Fatalf("unexpected config path: %s", paths.ConfigPath)
	}
	if paths.Channel != "dev" {
		t.Fatalf("unexpected channel: %s", paths.Channel)
	}
}
