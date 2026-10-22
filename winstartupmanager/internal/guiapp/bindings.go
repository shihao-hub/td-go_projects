package guiapp

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"winstartupmanager/internal/engine"
	"winstartupmanager/internal/logx"
	"winstartupmanager/internal/model"
)

type StartupAppService struct {
	app *App
}

func NewStartupAppService(app *App) *StartupAppService {
	return &StartupAppService{app: app}
}

// ListRegistryItems scans all physical registry startup locations.
func (s *StartupAppService) ListRegistryItems() ([]model.StartupItem, error) {
	return engine.ListRegistryItems()
}

// ListFolderItems lists items from Startup Folders.
func (s *StartupAppService) ListFolderItems() ([]model.StartupItem, error) {
	return engine.ListFolderItems()
}

// ListScheduledTasks lists boot/logon scheduled tasks.
func (s *StartupAppService) ListScheduledTasks() ([]model.StartupItem, error) {
	return engine.ListScheduledTasks()
}

// ToggleItem toggles the enabled state in StartupApproved\Run.
func (s *StartupAppService) ToggleItem(source model.StartupSource, name string, enable bool) error {
	return engine.ToggleItem(source, name, enable)
}

// DeleteItem removes a startup item with pre-delete backup.
func (s *StartupAppService) DeleteItem(source model.StartupSource, name string) error {
	return engine.DeleteItem(source, name)
}

// SaveItem adds or edits a startup item.
func (s *StartupAppService) SaveItem(source model.StartupSource, name string, exePath string, args string) error {
	return engine.SaveItem(source, name, exePath, args)
}

// LocateInExplorer opens file explorer and highlights the executable.
func (s *StartupAppService) LocateInExplorer(exePath string) error {
	return engine.LocateInExplorer(exePath)
}

// LocateInRegistry jumps to the item in regedit.exe.
func (s *StartupAppService) LocateInRegistry(source model.StartupSource, name string) error {
	return engine.LocateInRegistry(source, name)
}

// ExportBackup exports current registry items to the chosen file path.
func (s *StartupAppService) ExportBackup(destPath string) error {
	items, err := engine.ListRegistryItems()
	if err != nil {
		return err
	}
	return engine.ExportFullBackup(destPath, items)
}

// IsAdmin returns true if the current process is elevated.
func (s *StartupAppService) IsAdmin() bool {
	return engine.IsProcessElevated()
}

// RestartAsAdmin triggers UAC elevation and restarts.
func (s *StartupAppService) RestartAsAdmin() error {
	if err := engine.RestartAsAdmin(); err != nil {
		logx.Errorf("提权重启失败: %v", err)
		return err
	}
	return nil
}

var (
	frontendErrorMu   sync.Mutex
	frontendErrorSeen = make(map[string]time.Time)
)

// ReportFrontendError 把前端运行时异常写入同一份日志，带同消息 1 分钟去重。
func (s *StartupAppService) ReportFrontendError(message string, stack string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}

	key := message
	if len(key) > 200 {
		key = key[:200]
	}

	frontendErrorMu.Lock()
	now := time.Now()
	for k, seenAt := range frontendErrorSeen {
		if now.Sub(seenAt) >= time.Minute {
			delete(frontendErrorSeen, k)
		}
	}
	if _, exists := frontendErrorSeen[key]; exists {
		frontendErrorMu.Unlock()
		return
	}
	frontendErrorSeen[key] = now
	frontendErrorMu.Unlock()

	if len(stack) > 2000 {
		stack = stack[:2000]
	}
	logx.Errorf("[FE] %s\n%s", message, stack)
}

// SelectExeFileDialog opens a Windows native file picker for selecting an executable.
func (s *StartupAppService) SelectExeFileDialog() (string, error) {
	dlg := s.app.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("可执行文件 (*.exe;*.bat;*.cmd)", "*.exe;*.bat;*.cmd").
		AddFilter("所有文件 (*.*)", "*.*")

	path, err := dlg.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return path, nil
}

// SelectBackupSaveDialog opens a save file dialog for exporting backups.
func (s *StartupAppService) SelectBackupSaveDialog() (string, error) {
	dlg := s.app.app.Dialog.SaveFile().
		SetFilename("winstartupmanager_backup.reg").
		AddFilter("注册表脚本 (*.reg)", "*.reg").
		AddFilter("JSON 备份 (*.json)", "*.json")

	path, err := dlg.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return path, nil
}

// SelectBackupOpenDialog opens a file dialog for importing a backup.
func (s *StartupAppService) SelectBackupOpenDialog() (string, error) {
	dlg := s.app.app.Dialog.OpenFile().
		CanChooseFiles(true).
		AddFilter("备份文件 (*.reg;*.json)", "*.reg;*.json").
		AddFilter("所有文件 (*.*)", "*.*")

	path, err := dlg.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return path, nil
}

// ImportBackup imports entries from a .reg or .json backup file.
func (s *StartupAppService) ImportBackup(srcPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("读取备份文件失败: %w", err)
	}

	content := string(data)
	if strings.HasSuffix(strings.ToLower(srcPath), ".reg") {
		var currentKey string
		lines := strings.Split(content, "\n")
		count := 0
		for _, rawLine := range lines {
			line := strings.TrimSpace(rawLine)
			if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "Windows Registry") {
				continue
			}
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				currentKey = strings.Trim(line, "[]")
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				name := strings.Trim(parts[0], "\"")
				val := strings.Trim(parts[1], "\"")
				val = strings.ReplaceAll(val, `\\`, `\`)
				val = strings.ReplaceAll(val, `\"`, `"`)

				var src model.StartupSource
				switch {
				case strings.Contains(currentKey, `HKEY_CURRENT_USER`) && strings.Contains(currentKey, `RunOnce`):
					src = model.SourceRegistryHKCURunOnce
				case strings.Contains(currentKey, `HKEY_CURRENT_USER`):
					src = model.SourceRegistryHKCU
				case strings.Contains(currentKey, `WOW6432Node`):
					src = model.SourceRegistryHKLMWOW64
				case strings.Contains(currentKey, `HKEY_LOCAL_MACHINE`):
					src = model.SourceRegistryHKLM
				default:
					src = model.SourceRegistryHKCU
				}

				exe, args := engine.ParseCommandLine(val)
				if err := engine.SaveItem(src, name, exe, args); err == nil {
					count++
				}
			}
		}
		if count == 0 {
			return fmt.Errorf("未在文件中找到有效的启动项数据")
		}
		return nil
	}
	return fmt.Errorf("暂不支持该文件类型的恢复")
}
