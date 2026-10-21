package guiapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), windowStateFileName)
	want := windowState{Width: 1200, Height: 760, X: 120, Y: 90, HasPos: true, Maximised: true}

	if err := writeWindowStateTo(path, want); err != nil {
		t.Fatalf("写入窗口状态失败: %v", err)
	}
	got := loadWindowStateFrom(path)
	if got != want {
		t.Fatalf("往返不一致: got %+v, want %+v", got, want)
	}
}

func TestWindowStateMissingFileFallsBackToDefault(t *testing.T) {
	got := loadWindowStateFrom(filepath.Join(t.TempDir(), "absent.json"))
	if got != defaultWindowState() {
		t.Fatalf("缺失文件应回落默认值: %+v", got)
	}
}

func TestWindowStateDirtyJSONFallsBackToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), windowStateFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := loadWindowStateFrom(path); got != defaultWindowState() {
		t.Fatalf("脏 JSON 应回落默认值: %+v", got)
	}
}

func TestWindowStateTooSmallFallsBackToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), windowStateFileName)
	if err := writeWindowStateTo(path, windowState{Width: 320, Height: 200, X: 10, Y: 10, HasPos: true}); err != nil {
		t.Fatal(err)
	}
	if got := loadWindowStateFrom(path); got != defaultWindowState() {
		t.Fatalf("过小尺寸应回落默认值: %+v", got)
	}
}

func TestWindowStateWithoutPositionKeepsDefaultCentering(t *testing.T) {
	path := filepath.Join(t.TempDir(), windowStateFileName)
	if err := writeWindowStateTo(path, windowState{Width: 1100, Height: 720}); err != nil {
		t.Fatal(err)
	}
	got := loadWindowStateFrom(path)
	if got.HasPos {
		t.Fatalf("未记录坐标时 HasPos 必须为 false: %+v", got)
	}
	if got.Width != 1100 || got.Height != 720 {
		t.Fatalf("尺寸应保留: %+v", got)
	}
}

func TestWindowStateEmptyPathIsRejected(t *testing.T) {
	if err := writeWindowStateTo("", defaultWindowState()); err == nil {
		t.Fatal("空路径应返回错误")
	}
}
