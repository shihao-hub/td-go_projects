// window_state_test.go：窗口几何记忆的纯函数断言。
package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowState_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), windowStateFileName)
	want := windowState{Width: 1200, Height: 800, X: 120, Y: 80, HasPos: true, Maximised: true}
	if err := writeWindowState(path, want); err != nil {
		t.Fatalf("writeWindowState: %v", err)
	}
	if got := loadWindowState(path); got != want {
		t.Fatalf("loadWindowState = %+v, want %+v", got, want)
	}
}

func TestWindowState_MissingFileUsesDefaults(t *testing.T) {
	got := loadWindowState(filepath.Join(t.TempDir(), "missing.json"))
	if got.Width != defaultWindowWidth || got.Height != defaultWindowHeight || got.HasPos {
		t.Fatalf("defaults = %+v", got)
	}
}

func TestWindowState_DirtyJSONUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), windowStateFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadWindowState(path)
	if got.Width != defaultWindowWidth || got.Height != defaultWindowHeight {
		t.Fatalf("dirty json = %+v", got)
	}
}

func TestWindowState_TooSmallFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), windowStateFileName)
	if err := writeWindowState(path, windowState{Width: 10, Height: 10, HasPos: true}); err != nil {
		t.Fatal(err)
	}
	got := loadWindowState(path)
	if got.Width != defaultWindowWidth || got.Height != defaultWindowHeight {
		t.Fatalf("small size = %+v", got)
	}
}

func TestWindowState_VisibleOn(t *testing.T) {
	areas := []windowArea{{X: 0, Y: 0, Width: 1920, Height: 1080}}
	if !(windowState{X: 100, Y: 100, Width: 800, Height: 600, HasPos: true}.visibleOn(areas)) {
		t.Fatal("窗口在屏内应可见")
	}
	if (windowState{X: 5000, Y: 100, Width: 800, Height: 600, HasPos: true}.visibleOn(areas)) {
		t.Fatal("窗口在屏外应判为不可见")
	}
	if !(windowState{X: 5000, Y: 100, Width: 800, Height: 600}.visibleOn(areas)) {
		t.Fatal("无坐标时不应拦截")
	}
}
