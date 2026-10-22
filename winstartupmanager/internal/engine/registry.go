package engine

import (
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sys/windows/registry"
	"winstartupmanager/internal/logx"
	"winstartupmanager/internal/model"
)

type regLocation struct {
	source   model.StartupSource
	root     registry.Key
	path     string
	access   uint32
	elevated bool
}

var locations = []regLocation{
	{
		source:   model.SourceRegistryHKCU,
		root:     registry.CURRENT_USER,
		path:     `Software\Microsoft\Windows\CurrentVersion\Run`,
		access:   registry.READ,
		elevated: false,
	},
	{
		source:   model.SourceRegistryHKCURunOnce,
		root:     registry.CURRENT_USER,
		path:     `Software\Microsoft\Windows\CurrentVersion\RunOnce`,
		access:   registry.READ,
		elevated: false,
	},
	{
		source:   model.SourceRegistryHKLM,
		root:     registry.LOCAL_MACHINE,
		path:     `Software\Microsoft\Windows\CurrentVersion\Run`,
		access:   registry.READ,
		elevated: true,
	},
	{
		source:   model.SourceRegistryHKLMWOW64,
		root:     registry.LOCAL_MACHINE,
		path:     `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`,
		access:   registry.READ | registry.WOW64_32KEY,
		elevated: true,
	},
}

func getLocation(source model.StartupSource) (*regLocation, error) {
	for _, loc := range locations {
		if loc.source == source {
			return &loc, nil
		}
	}
	return nil, fmt.Errorf("未知的启动源类型: %s", source)
}

// ListRegistryItems scans all 4 physical registry locations and returns structured StartupItems.
func ListRegistryItems() ([]model.StartupItem, error) {
	var mu sync.Mutex
	var items []model.StartupItem
	var wg sync.WaitGroup

	for _, loc := range locations {
		wg.Add(1)
		go func(l regLocation) {
			defer wg.Done()
			subItems := scanLocation(l)
			if len(subItems) > 0 {
				mu.Lock()
				items = append(items, subItems...)
				mu.Unlock()
			}
		}(loc)
	}

	wg.Wait()
	return items, nil
}

func scanLocation(loc regLocation) []model.StartupItem {
	k, err := registry.OpenKey(loc.root, loc.path, loc.access)
	if err != nil {
		return nil
	}
	defer k.Close()

	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil
	}

	var results []model.StartupItem
	for _, name := range names {
		val, valType, err := k.GetStringValue(name)
		if err != nil {
			continue
		}
		if valType != registry.SZ && valType != registry.EXPAND_SZ {
			continue
		}

		exePath, args := ParseCommandLine(val)
		resolved, fileExists := resolveExePath(exePath)

		enabled, _ := IsStartupApproved(loc.source, name)
		publisher := ""
		if fileExists {
			publisher = GetFilePublisher(resolved)
		} else {
			publisher = "文件不存在"
		}

		results = append(results, model.StartupItem{
			ID:         model.GenerateID(loc.source, name),
			Source:     loc.source,
			Name:       name,
			RawCommand: val,
			ExePath:    exePath,
			Args:       args,
			IsEnabled:  enabled,
			FileExists: fileExists,
			Publisher:  publisher,
			IsElevated: loc.elevated,
		})
	}

	return results
}

// SaveItem creates or edits a startup item in the registry.
func SaveItem(source model.StartupSource, name string, exePath string, args string) error {
	name = strings.TrimSpace(name)
	exePath = strings.TrimSpace(exePath)
	args = strings.TrimSpace(args)

	if name == "" {
		return fmt.Errorf("启动项名称不能为空")
	}
	if exePath == "" {
		return fmt.Errorf("可执行文件路径不能为空")
	}

	loc, err := getLocation(source)
	if err != nil {
		return err
	}

	var rawCommand string
	cleanExe := strings.Trim(exePath, "\"")
	if strings.Contains(cleanExe, " ") {
		rawCommand = fmt.Sprintf("\"%s\"", cleanExe)
	} else {
		rawCommand = cleanExe
	}
	if args != "" {
		rawCommand += " " + args
	}

	writeAccess := uint32(registry.SET_VALUE | registry.QUERY_VALUE)
	if loc.source == model.SourceRegistryHKLMWOW64 {
		writeAccess |= registry.WOW64_32KEY
	}

	k, _, err := registry.CreateKey(loc.root, loc.path, writeAccess)
	if err != nil {
		if loc.elevated && !IsProcessElevated() {
			return fmt.Errorf("修改 HKLM 需要管理员权限，请点击上方“以管理员身份重启”后再试: %w", err)
		}
		return fmt.Errorf("打开注册表写入失败: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue(name, rawCommand); err != nil {
		return fmt.Errorf("写入注册表键值失败: %w", err)
	}

	// Default new entry to enabled in StartupApproved
	if err := SetStartupApproved(source, name, true); err != nil {
		logx.Warnf("新启动项默认启用写入失败 source=%s name=%s: %v", source, name, err)
	}
	return nil
}

// DeleteItem removes a startup entry, auto-saving a .reg backup beforehand and cleaning StartupApproved.
func DeleteItem(source model.StartupSource, name string) error {
	loc, err := getLocation(source)
	if err != nil {
		return err
	}

	readAccess := uint32(registry.READ)
	writeAccess := uint32(registry.SET_VALUE | registry.QUERY_VALUE)
	if loc.source == model.SourceRegistryHKLMWOW64 {
		readAccess |= registry.WOW64_32KEY
		writeAccess |= registry.WOW64_32KEY
	}

	// 1. Read existing value to create snapshot
	kRead, err := registry.OpenKey(loc.root, loc.path, readAccess)
	if err != nil {
		return fmt.Errorf("读取原注册表项失败: %w", err)
	}
	rawVal, _, err := kRead.GetStringValue(name)
	kRead.Close()
	if err != nil {
		return fmt.Errorf("原启动项不存在或已删除: %w", err)
	}

	// 2. Pre-delete backup
	if _, err := CreateSingleItemBackup(source, name, rawVal); err != nil {
		// Log or return backup failure
		return fmt.Errorf("预删除备份失败，已取消删除操作: %w", err)
	}

	// 3. Delete registry value
	kWrite, err := registry.OpenKey(loc.root, loc.path, writeAccess)
	if err != nil {
		if loc.elevated && !IsProcessElevated() {
			return fmt.Errorf("删除 HKLM 项需要管理员权限，请以管理员身份运行后再试: %w", err)
		}
		return fmt.Errorf("获取注册表删除权限失败: %w", err)
	}
	defer kWrite.Close()

	if err := kWrite.DeleteValue(name); err != nil {
		return fmt.Errorf("删除注册表项失败: %w", err)
	}

	// 4. Clean StartupApproved
	if err := DeleteStartupApproved(source, name); err != nil {
		logx.Warnf("清理 StartupApproved 失败 source=%s name=%s: %v", source, name, err)
	}
	return nil
}

// ToggleItem toggles the enabled state in StartupApproved without altering the Run command.
func ToggleItem(source model.StartupSource, name string, enable bool) error {
	loc, err := getLocation(source)
	if err != nil {
		return err
	}

	if loc.elevated && !IsProcessElevated() {
		return fmt.Errorf("修改系统级启动项需要管理员权限，请点击上方“以管理员身份重启”后再试")
	}

	return SetStartupApproved(source, name, enable)
}
