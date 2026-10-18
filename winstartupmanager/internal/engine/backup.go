package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"winstartupmanager/internal/model"
	"winstartupmanager/internal/store"
)

var invalidFilenameChars = regexp.MustCompile(`[\\/:*?"<>|]`)

func SanitizeFilename(name string) string {
	return invalidFilenameChars.ReplaceAllString(name, "_")
}

// RegistryPathForSource returns the full display path of the registry key for .reg files.
func RegistryPathForSource(source model.StartupSource) string {
	switch source {
	case model.SourceRegistryHKCU:
		return `HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run`
	case model.SourceRegistryHKCURunOnce:
		return `HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\RunOnce`
	case model.SourceRegistryHKLM:
		return `HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\Run`
	case model.SourceRegistryHKLMWOW64:
		return `HKEY_LOCAL_MACHINE\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`
	default:
		return `HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run`
	}
}

// EscapeRegString escapes backslashes and double quotes for .reg file values.
func EscapeRegString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// CreateSingleItemBackup creates an automatic .reg snapshot before deleting an item.
func CreateSingleItemBackup(source model.StartupSource, name string, rawCommand string) (string, error) {
	backupDir, err := store.BackupDir()
	if err != nil {
		return "", fmt.Errorf("获取备份目录失败: %w", err)
	}

	ts := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("backup_%s_%s_%s.reg", source, SanitizeFilename(name), ts)
	fullPath := filepath.Join(backupDir, filename)

	regKey := RegistryPathForSource(source)
	regContent := fmt.Sprintf("Windows Registry Editor Version 5.00\r\n\r\n[%s]\r\n\"%s\"=\"%s\"\r\n",
		regKey,
		EscapeRegString(name),
		EscapeRegString(rawCommand),
	)

	if err := os.WriteFile(fullPath, []byte(regContent), 0644); err != nil {
		return "", fmt.Errorf("写入备份文件失败: %w", err)
	}

	return fullPath, nil
}

// ExportFullBackup exports all registry items to a .reg or .json file.
func ExportFullBackup(destPath string, items []model.StartupItem) error {
	if strings.HasSuffix(strings.ToLower(destPath), ".json") {
		data, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0644)
	}

	// Default to .reg format
	var sb strings.Builder
	sb.WriteString("Windows Registry Editor Version 5.00\r\n\r\n")

	// Group items by source
	grouped := make(map[model.StartupSource][]model.StartupItem)
	for _, item := range items {
		grouped[item.Source] = append(grouped[item.Source], item)
	}

	sources := []model.StartupSource{
		model.SourceRegistryHKCU,
		model.SourceRegistryHKCURunOnce,
		model.SourceRegistryHKLM,
		model.SourceRegistryHKLMWOW64,
	}

	for _, src := range sources {
		itemList := grouped[src]
		if len(itemList) == 0 {
			continue
		}
		regKey := RegistryPathForSource(src)
		sb.WriteString(fmt.Sprintf("[%s]\r\n", regKey))
		for _, it := range itemList {
			sb.WriteString(fmt.Sprintf("\"%s\"=\"%s\"\r\n", EscapeRegString(it.Name), EscapeRegString(it.RawCommand)))
		}
		sb.WriteString("\r\n")
	}

	return os.WriteFile(destPath, []byte(sb.String()), 0644)
}