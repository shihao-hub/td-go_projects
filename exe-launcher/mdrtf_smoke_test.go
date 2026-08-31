package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRealMdFiles 对各子项目真实介绍文件跑转换：不 panic、RTF 闭合、含头部。
// 路径相对仓库布局硬编码，文件缺失时跳过（单机跑不保证兄弟项目都在）。
func TestRealMdFiles(t *testing.T) {
	base := filepath.Join("..")
	for _, name := range []string{
		filepath.Join("exe-launcher", "exe-launcher.md"),
		filepath.Join("agent-reaper", "agent-reaper.md"),
		filepath.Join("mcp-cleanup", "mcp-cleanup.md"),
		filepath.Join("file-sync", "file-sync.md"),
		filepath.Join("zread-tray", "zread-tray.md"),
		filepath.Join("console-calculator", "console-calculator.md"),
	} {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Logf("跳过 %s: %v", name, err)
			continue
		}
		got := mdToRTF(string(data))
		if !strings.HasPrefix(got, `{\rtf1`) || !strings.HasSuffix(got, "}") {
			t.Errorf("%s: RTF 未正确闭合/开头", name)
		}
		t.Logf("%s: %d 字节 md → %d 字节 RTF", name, len(data), len(got))
	}
}
