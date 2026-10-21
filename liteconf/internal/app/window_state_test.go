package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shihao-hub/liteconf/internal/appenv"
)

// useTempAppData 把数据目录重定向到临时目录，避免测试污染真实 APPDATA。
func useTempAppData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Cleanup(appenv.CloseLog)
	return dir
}

func TestWindowStateRoundTrip(t *testing.T) {
	useTempAppData(t)

	want := windowState{Width: 1366, Height: 900, X: 120, Y: 80, HasPos: true, Maximised: false}
	if err := writeWindowState(want); err != nil {
		t.Fatalf("writeWindowState: %v", err)
	}

	got := loadWindowState()
	if got != want {
		t.Fatalf("往返不一致: got %+v, want %+v", got, want)
	}
}

func TestWindowStateMissingFileFallsBackToDefault(t *testing.T) {
	useTempAppData(t)

	got := loadWindowState()
	if got.Width != defaultWindowWidth || got.Height != defaultWindowHeight {
		t.Fatalf("无文件应回落默认尺寸，得到 %+v", got)
	}
	if got.HasPos || got.Maximised {
		t.Fatalf("无文件不应带位置/最大化标记，得到 %+v", got)
	}
}

func TestWindowStateCorruptJSONFallsBackToDefault(t *testing.T) {
	useTempAppData(t)

	path := windowStatePath()
	if path == "" {
		t.Fatal("windowStatePath 为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadWindowState()
	if got.Width != defaultWindowWidth || got.Height != defaultWindowHeight {
		t.Fatalf("脏 JSON 应回落默认尺寸，得到 %+v", got)
	}
}

func TestWindowStateTinySizeRejected(t *testing.T) {
	useTempAppData(t)

	path := windowStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(windowState{Width: 200, Height: 100, X: 10, Y: 10, HasPos: true})
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadWindowState()
	if got.Width != defaultWindowWidth || got.Height != defaultWindowHeight {
		t.Fatalf("过小尺寸应回落默认尺寸，得到 %+v", got)
	}
	// 尺寸被拒但位置仍可合法恢复（用户可能只是窗口被压小了）。
	if !got.HasPos || got.X != 10 || got.Y != 10 {
		t.Fatalf("位置应保留，得到 %+v", got)
	}
}

func TestWindowStateWithoutPositionDoesNotRestoreXY(t *testing.T) {
	useTempAppData(t)

	path := windowStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(windowState{Width: 1200, Height: 800, X: 50, Y: 50, HasPos: false})
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadWindowState()
	if got.HasPos {
		t.Fatalf("无坐标标记时不应恢复位置，得到 %+v", got)
	}
	if got.Width != 1200 || got.Height != 800 {
		t.Fatalf("尺寸应恢复，得到 %+v", got)
	}
}
