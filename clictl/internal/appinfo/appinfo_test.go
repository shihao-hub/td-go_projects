package appinfo

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateConfig 把配置根重定向到测试临时目录，避免污染真实 %APPDATA%。
// Logf 会把日志文件句柄保持到进程退出，t.TempDir 的严格清理会因此报错，
// 故手动建临时目录并在清理失败时忽略。
func isolateConfig(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "clictl-appinfo-test-")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	return dir
}

func TestDataDirIsChannelScoped(t *testing.T) {
	dir := isolateConfig(t)
	got := DataDir()
	want := filepath.Join(dir, "language_projects", AppName, Channel())
	if got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
}

func TestDefaultDBPathMigratesLegacyOnce(t *testing.T) {
	dir := isolateConfig(t)

	legacy := filepath.Join(dir, "language_projects", AppName, "clictl.db")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatalf("创建旧库目录失败: %v", err)
	}
	if err := os.WriteFile(legacy, []byte("legacy-content"), 0o644); err != nil {
		t.Fatalf("写入旧库失败: %v", err)
	}

	target := DefaultDBPath()
	if target != filepath.Join(DataDir(), "clictl.db") {
		t.Fatalf("DefaultDBPath() = %q, 应为通道目录下的 clictl.db", target)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("读取迁移结果失败: %v", err)
	}
	if string(raw) != "legacy-content" {
		t.Fatalf("迁移内容不一致: %q", raw)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("旧库应保留: %v", err)
	}

	// 已存在目标时不得覆盖（迁移只做一次）。
	if err := os.WriteFile(target, []byte("new-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = DefaultDBPath()
	raw, _ = os.ReadFile(target)
	if string(raw) != "new-content" {
		t.Fatalf("目标已存在时不应被旧库覆盖: %q", raw)
	}
}

func TestDefaultDBPathWithoutLegacyCreatesNothing(t *testing.T) {
	isolateConfig(t)
	target := DefaultDBPath()
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("无旧库时不应凭空创建目标库: %v", err)
	}
}
