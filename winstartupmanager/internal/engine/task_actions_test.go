package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"winstartupmanager/internal/store"
)

// runSchTasks 执行 schtasks，忽略输出（GBK 编码），只看退出码。
func runSchTasks(args ...string) error {
	return exec.Command("schtasks", args...).Run()
}

// taskState 用 PowerShell 查询计划任务状态；仅识别 TaskState 枚举字，
// cmdlet 报错（含 ObjectNotFound）不设非零退出码，故统一按不存在返回空串。
func taskState(_ *testing.T, name string) string {
	out, _ := exec.Command(
		"powershell", "-NoProfile", "-Command",
		fmt.Sprintf("(Get-ScheduledTask -TaskName %q).State", name),
	).CombinedOutput()
	s := string(out)
	for _, valid := range []string{"Unknown", "Disabled", "Queued", "Ready", "Running", "StartPending"} {
		if strings.Contains(s, valid) {
			return valid
		}
	}
	return ""
}

// createProbeTask 创建一个每小时触发的真实探测任务。
func createProbeTask(t *testing.T, name string) {
	t.Helper()
	if err := runSchTasks(
		"/create", "/tn", name, "/sc", "hourly",
		"/tr", "cmd /c exit 0", "/f",
	); err != nil {
		t.Fatalf("创建探测任务失败: %v", err)
	}
}

// TestTaskActionsRoundTrip 走真实 COM：启用 ↔ 停用 ↔ 删除（含 XML 备份落盘）。
func TestTaskActionsRoundTrip(t *testing.T) {
	name := fmt.Sprintf("wsm_toggle_probe_%d", time.Now().UnixMilli())
	taskPath := `\` + name
	createProbeTask(t, name)
	defer func() {
		// 兜底清理：若测试中途失败，确保不遗留探测任务
		if taskState(t, name) != "" {
			_ = runSchTasks("/delete", "/tn", name, "/f")
		}
	}()

	backupDir, err := store.BackupDir()
	if err != nil {
		t.Fatalf("获取备份目录失败: %v", err)
	}

	if err := ToggleTask(taskPath, false); err != nil {
		t.Fatalf("停用任务失败: %v", err)
	}
	if got := taskState(t, name); got != "Disabled" {
		t.Fatalf("停用后状态 = %q, want Disabled", got)
	}

	if err := ToggleTask(taskPath, true); err != nil {
		t.Fatalf("重新启用任务失败: %v", err)
	}
	if got := taskState(t, name); got != "Ready" {
		t.Fatalf("启用后状态 = %q, want Ready", got)
	}

	before := time.Now()
	if err := DeleteTask(taskPath); err != nil {
		t.Fatalf("删除任务失败: %v", err)
	}
	if state := taskState(t, name); state != "" {
		t.Fatalf("删除后任务仍存在: %q", state)
	}

	// 校验删除前的 XML 快照真实落盘且内容是任务定义
	matched := filepath.Join(
		backupDir,
		fmt.Sprintf("backup_TaskScheduler_%s_*.xml", SanitizeFilename(strings.TrimPrefix(taskPath, `\`))),
	)
	tmpMatches, err := filepath.Glob(matched)
	if err != nil {
		t.Fatalf("扫描备份目录失败: %v", err)
	}
	found := false
	for _, m := range tmpMatches {
		fi, statErr := os.Stat(m)
		if statErr != nil || fi.ModTime().Before(before) {
			continue
		}
		data, readErr := os.ReadFile(m)
		if readErr != nil {
			t.Fatalf("读取备份失败 %s: %v", m, readErr)
		}
		if !strings.Contains(string(data), "<Task") && !strings.Contains(string(data), "<?xml") {
			t.Fatalf("备份内容不是任务定义 XML: %s", m)
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("未找到本次删除生成的备份文件: %s", matched)
	}
}

// TestCleanupStagingVerifier 清理此前验收遗留的 wsm_backup_verifier（若存在）。
func TestCleanupStagingVerifier(t *testing.T) {
	if taskState(t, "wsm_backup_verifier") == "" {
		t.Log("wsm_backup_verifier 已不存在，无需清理")
		return
	}
	if err := DeleteTask(`\wsm_backup_verifier`); err != nil {
		t.Fatalf("清理遗留任务失败: %v", err)
	}
	if state := taskState(t, "wsm_backup_verifier"); state != "" {
		t.Fatalf("清理后任务仍存在: %q", state)
	}
}
