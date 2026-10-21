package app

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateDataDir 把运行时数据目录重定向到测试临时目录，避免污染真实 dev/prod 数据。
// 手动管理临时目录：logf 会把日志文件句柄保持到进程退出，
// t.TempDir 的严格清理会因句柄占用而报错，故这里清理失败时只忽略。
func isolateDataDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "anatomymanager-test-")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	if got := windowStatePath(); got == "" {
		t.Fatalf("窗口状态路径为空，数据目录未生效")
	}
	return dir
}

func TestWindowStateRoundTrip(t *testing.T) {
	isolateDataDir(t)

	saved := windowState{Width: 1440, Height: 900, X: 120, Y: 80, HasPos: true, Maximised: true}
	if err := writeWindowState(saved); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	got := loadWindowState()
	if got != saved {
		t.Fatalf("读回状态不一致: got %+v, want %+v", got, saved)
	}
}

func TestLoadWindowStateWithoutFileUsesDefaults(t *testing.T) {
	isolateDataDir(t)

	got := loadWindowState()
	want := defaultWindowState()
	if got != want {
		t.Fatalf("无文件时应回落默认值: got %+v, want %+v", got, want)
	}
}

func TestLoadWindowStateRejectsCorruptFile(t *testing.T) {
	isolateDataDir(t)

	if err := os.MkdirAll(filepath.Dir(windowStatePath()), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(windowStatePath(), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("写入脏数据失败: %v", err)
	}

	got := loadWindowState()
	if got != defaultWindowState() {
		t.Fatalf("损坏文件应回落默认值: got %+v", got)
	}
}

func TestLoadWindowStateRejectsDegenerateSize(t *testing.T) {
	isolateDataDir(t)

	if err := writeWindowState(windowState{Width: 10, Height: 10}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	got := loadWindowState()
	if got.Width != defaultWindowWidth || got.Height != defaultWindowHeight {
		t.Fatalf("过小尺寸应回落默认值: got %+v", got)
	}
}

func TestLoadWindowStateDropsPositionWhenUnset(t *testing.T) {
	isolateDataDir(t)

	if err := writeWindowState(windowState{Width: 1000, Height: 700}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	got := loadWindowState()
	if got.HasPos {
		t.Fatalf("未记录坐标时不应恢复位置: got %+v", got)
	}
	if got.Width != 1000 || got.Height != 700 {
		t.Fatalf("尺寸应被恢复: got %+v", got)
	}
}
