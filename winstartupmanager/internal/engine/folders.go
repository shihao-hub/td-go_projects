package engine

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows/registry"
	"winstartupmanager/internal/model"
)

const (
	hkcuStartupApprovedFolder = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\StartupFolder`
	hklmStartupApprovedFolder = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\StartupFolder`
)

func createDispatch(progID string) (*ole.IDispatch, error) {
	unknown, err := oleutil.CreateObject(progID)
	if err != nil {
		return nil, err
	}
	defer unknown.Release()
	return unknown.QueryInterface(ole.IID_IDispatch)
}

// ListFolderItems enumerates startup items in User and Common Startup folders.
func ListFolderItems() ([]model.StartupItem, error) {
	var items []model.StartupItem

	userDir := filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Startup`)
	commonDir := filepath.Join(os.Getenv("ProgramData"), `Microsoft\Windows\Start Menu\Programs\Startup`)

	userItems := scanStartupFolder(userDir, model.SourceStartupFolderUser, registry.CURRENT_USER, hkcuStartupApprovedFolder)
	items = append(items, userItems...)

	commonItems := scanStartupFolder(commonDir, model.SourceStartupFolderCommon, registry.LOCAL_MACHINE, hklmStartupApprovedFolder)
	items = append(items, commonItems...)

	return items, nil
}

func scanStartupFolder(dir string, source model.StartupSource, root registry.Key, approvedSubkey string) []model.StartupItem {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	_ = ole.CoInitialize(0)
	defer ole.CoUninitialize()

	wscript, err := createDispatch("WScript.Shell")
	if err == nil && wscript != nil {
		defer wscript.Release()
	}

	var results []model.StartupItem
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.EqualFold(name, "desktop.ini") {
			continue
		}

		fullPath := filepath.Join(dir, name)
		exePath := fullPath
		args := ""

		if strings.EqualFold(filepath.Ext(name), ".lnk") && wscript != nil {
			target, arguments := resolveShortcut(wscript, fullPath)
			if target != "" {
				exePath = target
				args = arguments
			}
		}

		cleanExe := cleanExePath(exePath)
		resolved, fileExists := resolveExePath(cleanExe)

		enabled := isFolderItemApproved(root, approvedSubkey, name)
		publisher := ""
		if fileExists {
			publisher = GetFilePublisher(resolved)
		} else {
			publisher = "文件不存在"
		}

		displayName := strings.TrimSuffix(name, filepath.Ext(name))
		rawCommand := fullPath
		if cleanExe != "" && cleanExe != fullPath {
			rawCommand = cleanExe
			if args != "" {
				rawCommand += " " + args
			}
		}

		results = append(results, model.StartupItem{
			ID:         model.GenerateID(source, name),
			Source:     source,
			Name:       displayName,
			RawCommand: rawCommand,
			ExePath:    cleanExe,
			Args:       args,
			IsEnabled:  enabled,
			FileExists: fileExists,
			Publisher:  publisher,
			IsElevated: source == model.SourceStartupFolderCommon,
		})
	}

	return results
}

func resolveShortcut(wscript *ole.IDispatch, lnkPath string) (target string, args string) {
	defer func() {
		_ = recover()
	}()

	res, err := oleutil.CallMethod(wscript, "CreateShortcut", lnkPath)
	if err != nil || res == nil || res.Value() == nil {
		return "", ""
	}
	shortcut := res.ToIDispatch()
	defer shortcut.Release()

	targetProp, err := oleutil.GetProperty(shortcut, "TargetPath")
	if err == nil && targetProp.Value() != nil {
		target = targetProp.ToString()
	}
	argsProp, err := oleutil.GetProperty(shortcut, "Arguments")
	if err == nil && argsProp.Value() != nil {
		args = argsProp.ToString()
	}

	return target, args
}

func isFolderItemApproved(root registry.Key, path string, name string) bool {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return true
	}
	defer k.Close()

	val, _, err := k.GetBinaryValue(name)
	if err != nil {
		return true
	}
	return ParseStartupApprovedBytes(val)
}