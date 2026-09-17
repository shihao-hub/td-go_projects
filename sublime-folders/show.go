package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// showText 把文本写入固定临时文件并用记事本打开（zread-tray openLog 模式）。
func showText(name, header string, lines []string) {
	dir := filepath.Join(os.TempDir(), "sublime-folders")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, name+".txt")
	var b strings.Builder
	b.WriteString("\xEF\xBB\xBF") // UTF-8 BOM，兼容旧版记事本
	b.WriteString(header)
	b.WriteString("\r\n")
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\r\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		AlertError("sublime-folders", "写入临时文件失败: "+err.Error())
		return
	}
	_ = exec.Command("notepad", path).Start()
}

func showCurrent() {
	folders, src, err := CurrentFolders()
	if err != nil {
		AlertError("sublime-folders", "读取 Sublime 会话失败: "+err.Error())
		return
	}
	lines := []string{
		fmt.Sprintf("采集时间: %s", time.Now().Format("2006-01-02 15:04:05")),
		fmt.Sprintf("会话来源: %s", src),
		fmt.Sprintf("共 %d 个目录:", len(folders)),
	}
	for _, p := range folders {
		lines = append(lines, "  "+p)
	}
	showText("current", "Sublime Text 当前打开的目录", lines)
}

func showRecords(title, name string, snaps []snapshot) {
	lines := []string{fmt.Sprintf("共 %d 条记录（新的在前）", len(snaps))}
	for _, sn := range snaps {
		lines = append(lines, "", fmt.Sprintf("── %s · %d 个目录 ──", sn.TS, len(sn.Folders)))
		for _, p := range sn.Folders {
			lines = append(lines, "    "+p)
		}
	}
	showText(name, title, lines)
}
