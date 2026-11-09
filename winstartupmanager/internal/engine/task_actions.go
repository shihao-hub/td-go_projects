package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
	"winstartupmanager/internal/store"
)

// taskServiceConnect 建立 Task Scheduler COM 连接，返回 service dispatch（调用方负责 Release）。
func taskServiceConnect() (*ole.IDispatch, error) {
	service, err := createDispatch("Schedule.Service")
	if err != nil {
		return nil, err
	}
	if _, err := oleutil.CallMethod(service, "Connect"); err != nil {
		service.Release()
		return nil, err
	}
	return service, nil
}

// splitTaskPath 把完整任务路径（\dir\name）拆为 folder 路径与任务名。
func splitTaskPath(taskPath string) (folder string, name string, err error) {
	taskPath = strings.TrimSpace(taskPath)
	if taskPath == "" {
		return "", "", fmt.Errorf("任务路径为空")
	}
	normalized := strings.TrimPrefix(taskPath, `\`)
	if idx := strings.LastIndex(normalized, `\`); idx >= 0 {
		return `\` + normalized[:idx], normalized[idx+1:], nil
	}
	return `\`, normalized, nil
}

// ToggleTask 启停计划任务，语义等同任务管理器/任务计划程序的无损启停。
// COM 回写路径（Definition.Settings.Enabled + RegisterTaskDefinition 更新）
// 在 go-ole 上会因空凭据参数（VT_EMPTY）panic（invoke "unknown type"），故改用
// 无窗口的 schtasks.exe 子进程：/ENABLE 与 /DISABLE 同样只翻转 Enabled 位，
// 不触碰任务定义其余配置。
func ToggleTask(taskPath string, enable bool) error {
	action := "/DISABLE"
	if enable {
		action = "/ENABLE"
	}
	cmd := exec.Command("schtasks.exe", "/Change", "/TN", taskPath, action)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /Change 失败: %w: %s", err, decodeOEMConsole(output))
	}
	return nil
}

// decodeOEMConsole 把控制台程序输出的 OEM 代码页字节（中文系统为 GBK）转为 UTF-8。
func decodeOEMConsole(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	const cpOEM = 1
	needed, err := windows.MultiByteToWideChar(cpOEM, 0, &data[0], int32(len(data)), nil, 0)
	if err != nil || needed <= 0 {
		return string(data)
	}
	buffer := make([]uint16, needed)
	written, err := windows.MultiByteToWideChar(cpOEM, 0, &data[0], int32(len(data)), &buffer[0], needed)
	if err != nil || written <= 0 {
		return string(data)
	}
	return windows.UTF16ToString(buffer[:written])
}

// DeleteTask 删除计划任务；删除前把任务定义 XML 快照落 backups 目录，备份失败则中止删除。
func DeleteTask(taskPath string) error {
	// COM 初始化线程亲和：先锁住当前线程再初始化，防止 goroutine 迁移丢失 COM 状态
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	_ = ole.CoInitialize(0)
	defer ole.CoUninitialize()

	folderPath, taskName, err := splitTaskPath(taskPath)
	if err != nil {
		return err
	}

	xml, err := taskDefinitionXml(taskPath)
	if err != nil {
		return fmt.Errorf("备份失败，已中止删除: %w", err)
	}

	// 备份快照与注册表 .reg 同目录同命名风格：backup_<来源>_<名>_<时间戳>
	backupDir, err := store.BackupDir()
	if err != nil {
		return fmt.Errorf("获取备份目录失败: %w", err)
	}
	ts := time.Now().Format("20060102_150405")
	stem := SanitizeFilename(strings.TrimPrefix(taskPath, `\`))
	filename := fmt.Sprintf("backup_TaskScheduler_%s_%s.xml", stem, ts)
	if err := os.WriteFile(filepath.Join(backupDir, filename), []byte(xml), 0644); err != nil {
		return fmt.Errorf("写入任务备份失败: %w", err)
	}

	service, err := taskServiceConnect()
	if err != nil {
		return fmt.Errorf("连接任务计划服务失败: %w", err)
	}
	defer service.Release()

	folderRes, err := oleutil.CallMethod(service, "GetFolder", folderPath)
	if err != nil {
		return fmt.Errorf("找不到任务目录 %s: %w", folderPath, err)
	}
	folder := folderRes.ToIDispatch()
	defer folder.Release()

	if _, err := oleutil.CallMethod(folder, "DeleteTask", taskName, 0); err != nil {
		return fmt.Errorf("删除任务失败: %w", err)
	}
	return nil
}

// taskDefinitionXml 读取任务的完整定义 XML 文本（删除前快照）。
func taskDefinitionXml(taskPath string) (string, error) {
	folderPath, taskName, err := splitTaskPath(taskPath)
	if err != nil {
		return "", err
	}

	service, err := taskServiceConnect()
	if err != nil {
		return "", fmt.Errorf("连接任务计划服务失败: %w", err)
	}
	defer service.Release()

	folderRes, err := oleutil.CallMethod(service, "GetFolder", folderPath)
	if err != nil {
		return "", fmt.Errorf("找不到任务目录 %s: %w", folderPath, err)
	}
	folder := folderRes.ToIDispatch()
	defer folder.Release()

	taskRes, err := oleutil.CallMethod(folder, "GetTask", taskName)
	if err != nil {
		return "", fmt.Errorf("找不到任务 %s: %w", taskName, err)
	}
	task := taskRes.ToIDispatch()
	defer task.Release()

	defRes, err := oleutil.GetProperty(task, "Definition")
	if err != nil || defRes.Value() == nil {
		return "", fmt.Errorf("读取任务定义失败: %w", err)
	}
	definition := defRes.ToIDispatch()
	defer definition.Release()

	if xml := dispatchString(definition, "XmlText"); strings.TrimSpace(xml) != "" {
		return xml, nil
	}
	return "", fmt.Errorf("任务定义 XML 为空")
}
