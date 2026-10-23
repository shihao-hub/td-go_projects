package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"taskbarguard/internal/core"
)

func iconTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	appDir := filepath.Join(root, "antigravity")
	if err := os.MkdirAll(filepath.Join(appDir, "custom_icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 'P', 'N', 'G'}
	if err := os.WriteFile(filepath.Join(appDir, "custom_icons", "underlit_chrome.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}
	// 款式枚举以 .ico 为准，预览图用同名 .png
	if err := os.WriteFile(filepath.Join(appDir, "custom_icons", "underlit_chrome.ico"), []byte("ICO"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := core.New(core.Paths{Channel: "dev", ScriptsDir: root})
	server := httptest.NewServer(NewServer(service, Meta{}).Handler())
	t.Cleanup(server.Close)
	return server, root
}

func TestAppIconFallsBackToIcoWhenPngMissing(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "vscode")
	if err := os.MkdirAll(filepath.Join(appDir, "custom_icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "patch_vscode_icon.py"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "custom_icons", "silver.ico"), []byte("ICO"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := core.New(core.Paths{Channel: "dev", ScriptsDir: root})
	server := httptest.NewServer(NewServer(service, Meta{}).Handler())
	t.Cleanup(server.Close)
	resp, err := http.Get(server.URL + "/api/apps/vscode/icon?style=silver")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "ICO" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
}

func TestAppIconReturnsPNG(t *testing.T) {
	server, _ := iconTestServer(t)
	resp, err := http.Get(server.URL + "/api/apps/antigravity/icon?style=underlit_chrome")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 4 || string(body) != string([]byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func TestAppIconRejectsPathTraversal(t *testing.T) {
	server, _ := iconTestServer(t)
	for _, style := range []string{"..%2F..%2Fconfig.json", "a%2Fb", "a%5Cb"} {
		resp, err := http.Get(server.URL + "/api/apps/antigravity/icon?style=" + style)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("style %q: status = %d, want 400", style, resp.StatusCode)
		}
	}
}

func TestAppIconRejectsUnknownStyle(t *testing.T) {
	server, _ := iconTestServer(t)
	resp, err := http.Get(server.URL + "/api/apps/antigravity/icon?style=nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
