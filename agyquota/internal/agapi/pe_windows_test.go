//go:build windows

package agapi

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// getTestPEBinary 寻找本机可用的真实 PE 可执行文件用于测试
func getTestPEBinary(t *testing.T) string {
	t.Helper()
	// 优先使用 agy.exe
	if p, err := exec.LookPath("agy.exe"); err == nil {
		return p
	}
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		candidate := filepath.Join(localAppData, "agy", "bin", "agy.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// 回退自身测试二进制
	self, err := os.Executable()
	if err == nil {
		return self
	}
	t.Skip("未找到可用于测试的 PE 可执行文件")
	return ""
}

func TestReadPESubsystem(t *testing.T) {
	pePath := getTestPEBinary(t)
	subsystem, err := readPESubsystem(pePath)
	if err != nil {
		t.Fatalf("readPESubsystem 失败: %v", err)
	}
	// 控制台应用为 3，GUI 为 2
	if subsystem != 2 && subsystem != 3 {
		t.Fatalf("非预期的 Subsystem: %d", subsystem)
	}
}

func TestCalcFileSHA256(t *testing.T) {
	pePath := getTestPEBinary(t)
	hash1, err := calcFileSHA256(pePath)
	if err != nil {
		t.Fatalf("calcFileSHA256 失败: %v", err)
	}
	if len(hash1) != 64 {
		t.Fatalf("SHA-256 哈希长度错误: %s", hash1)
	}

	// 验证哈希正确性
	f, err := os.Open(pePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	hasher := sha256.New()
	_, _ = io.Copy(hasher, f)
	expected := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(hash1, expected) {
		t.Fatalf("哈希不匹配: got %s, expected %s", hash1, expected)
	}
}

func TestEnsureSilentAgyExe_FailNoFallback(t *testing.T) {
	// 1. 空源路径
	path, err := ensureSilentAgyExe("")
	if err == nil {
		t.Fatal("期望空路径报错，但未报错")
	}
	if path != "" {
		t.Fatalf("失败时禁止返回非空路径，got: %s", path)
	}

	// 2. 不存在的源路径
	path, err = ensureSilentAgyExe(`C:\path\to\nonexistent_agy_12345.exe`)
	if err == nil {
		t.Fatal("期望不存在的文件报错，但未报错")
	}
	if path != "" {
		t.Fatalf("失败时禁止回退源路径，got: %s", path)
	}

	// 3. 非 PE 垃圾文件
	tmpFile := filepath.Join(t.TempDir(), "fake_agy.exe")
	if err := os.WriteFile(tmpFile, []byte("this is not a pe file"), 0755); err != nil {
		t.Fatal(err)
	}
	path, err = ensureSilentAgyExe(tmpFile)
	if err == nil {
		t.Fatal("期望非 PE 文件报错，但未报错")
	}
	if path != "" {
		t.Fatalf("失败时禁止回退源路径，got: %s", path)
	}
}

func TestEnsureSilentAgyExe_SuccessAndCacheValidation(t *testing.T) {
	srcPE := getTestPEBinary(t)

	// 重定向 APPDATA 到隔离目录
	isolatedData := filepath.Join(t.TempDir(), "isolated_appdata")
	t.Setenv("APPDATA", isolatedData)

	dstPath, err := ensureSilentAgyExe(srcPE)
	if err != nil {
		t.Fatalf("ensureSilentAgyExe 失败: %v", err)
	}

	// 校验返回的文件确实存在
	fi, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("生成的静默副本不存在: %v", err)
	}
	if fi.IsDir() || !fi.Mode().IsRegular() {
		t.Fatalf("生成的静默副本不是常规文件")
	}

	// 校验 Subsystem 确实为 2
	subsystem, err := readPESubsystem(dstPath)
	if err != nil {
		t.Fatalf("读取静默副本 Subsystem 失败: %v", err)
	}
	if subsystem != 2 {
		t.Fatalf("静默副本 Subsystem 期望为 2，实际为 %d", subsystem)
	}

	// 校验 sha256 文件存在且与源文件匹配
	shaPath := dstPath + ".sha256"
	shaBytes, err := os.ReadFile(shaPath)
	if err != nil {
		t.Fatalf("读取 sha256 校验文件失败: %v", err)
	}
	expectedHash, err := calcFileSHA256(srcPE)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(strings.TrimSpace(string(shaBytes)), expectedHash) {
		t.Fatalf("绑定的 SHA-256 不匹配: got %s, expected %s", string(shaBytes), expectedHash)
	}

	// 再次调用，应该命中缓存且不报错
	dstPath2, err := ensureSilentAgyExe(srcPE)
	if err != nil {
		t.Fatalf("缓存命中再次调用失败: %v", err)
	}
	if dstPath2 != dstPath {
		t.Fatalf("缓存命中返回路径不一致: %s vs %s", dstPath2, dstPath)
	}

	// 测试篡改自愈：人为破坏 sha256 文件
	_ = os.WriteFile(shaPath, []byte("tampered_hash\n"), 0644)
	dstPath3, err := ensureSilentAgyExe(srcPE)
	if err != nil {
		t.Fatalf("sha256 篡改后自愈生成失败: %v", err)
	}
	newShaBytes, _ := os.ReadFile(shaPath)
	if !strings.EqualFold(strings.TrimSpace(string(newShaBytes)), expectedHash) {
		t.Fatalf("自愈后绑定的 SHA-256 未恢复: got %s, expected %s", string(newShaBytes), expectedHash)
	}
	if dstPath3 != dstPath {
		t.Fatalf("自愈后路径不一致: %s vs %s", dstPath3, dstPath)
	}
}
