package core

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiscoverApps 扫描脚本根目录下的应用子目录，返回可调度的应用清单。
func DiscoverApps(scriptsDir string) ([]AppInfo, error) {
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return nil, newError("list_failed", "无法读取脚本目录 %q: %v", scriptsDir, err)
	}
	apps := make([]AppInfo, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		app, err := InspectApp(filepath.Join(scriptsDir, entry.Name()), entry.Name())
		if err != nil {
			return nil, err
		}
		if app.Script != "" || len(app.Styles) > 0 {
			apps = append(apps, app)
		}
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps, nil
}

// NormalizeAppName 把常见别名归一为脚本目录名（wt / terminal / powershell）。
func NormalizeAppName(name string) string {
	normalized := strings.ToLower(strings.TrimSuffix(name, ".exe"))
	switch normalized {
	case "wt", "terminal", "windowsterminal", "windows_terminal", "powershell":
		return "wt"
	default:
		return name
	}
}

// FindApp 按名称（含别名）定位应用；不存在时返回 app_not_found。
func FindApp(scriptsDir, name string) (AppInfo, error) {
	resolved := NormalizeAppName(name)
	app, err := InspectApp(filepath.Join(scriptsDir, resolved), resolved)
	if err != nil {
		if ErrorCode(err) == "app_not_found" {
			return AppInfo{}, newError("app_not_found", "未找到应用: %s", name)
		}
		return AppInfo{}, err
	}
	if app.Script == "" && len(app.Styles) == 0 {
		return AppInfo{}, newError("app_not_found", "未找到应用: %s", name)
	}
	return app, nil
}

// InspectApp 读取单个应用目录：补丁脚本路径与全部可用款式。
func InspectApp(dir, name string) (AppInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return AppInfo{}, newError("app_not_found", "未找到应用目录: %s", name)
		}
		return AppInfo{}, err
	}
	app := AppInfo{Name: name, Styles: []string{}}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		file := entry.Name()
		if strings.HasPrefix(file, "patch_") && strings.HasSuffix(file, ".py") {
			app.Script = filepath.Join(dir, file)
		}
		if strings.HasPrefix(file, "vscode_dark_") && strings.HasSuffix(file, ".ico") {
			app.Styles = append(app.Styles, strings.TrimSuffix(strings.TrimPrefix(file, "vscode_dark_"), ".ico"))
		}
	}
	customIconDir := filepath.Join(dir, "custom_icons")
	if iconEntries, readErr := os.ReadDir(customIconDir); readErr == nil {
		for _, iconEntry := range iconEntries {
			if iconEntry.IsDir() || !strings.HasSuffix(strings.ToLower(iconEntry.Name()), ".ico") {
				continue
			}
			app.Styles = append(app.Styles, strings.TrimSuffix(iconEntry.Name(), filepath.Ext(iconEntry.Name())))
		}
	}
	sort.Strings(app.Styles)
	return app, nil
}

// Contains 判断字符串切片是否包含目标值。
func Contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// Paths 一次运行使用到的目录与配置落点。
type Paths struct {
	Channel    string `json:"channel"`
	ScriptsDir string `json:"scriptsDir"`
	ConfigPath string `json:"configPath"`
	DataDir    string `json:"dataDir"`
}

// DataDir 返回当前通道的运行时数据目录：
// %APPDATA%\language_projects\taskbarguard\<channel>，取不到时回退 ~/.language_projects/...
func DataDir(channel string) string {
	if base, err := os.UserConfigDir(); err == nil && base != "" {
		return filepath.Join(base, "language_projects", ToolName, channel)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".language_projects", ToolName, channel)
	}
	return ""
}

// ResolvePaths 计算本次运行的脚本目录与配置路径。
//
// 显式覆盖优先；否则 scripts 取「可执行文件同级 /scripts」（不存在时回退工作目录），
// config 取数据目录，并在首次运行时把 exe 同级或工作目录里的旧 config.json 复制过去。
func ResolvePaths(channel, scriptsOverride, configOverride string) Paths {
	exeDir := executableDir()
	cwd, _ := os.Getwd()

	paths := Paths{Channel: channel}
	paths.DataDir = DataDir(channel)

	if scriptsOverride != "" {
		paths.ScriptsDir = scriptsOverride
	} else {
		paths.ScriptsDir = DefaultScriptsDir
		if exeDir != "" {
			candidate := filepath.Join(exeDir, DefaultScriptsDir)
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				paths.ScriptsDir = candidate
			} else if cwd != "" {
				paths.ScriptsDir = filepath.Join(cwd, DefaultScriptsDir)
			}
		}
	}

	if configOverride != "" {
		paths.ConfigPath = configOverride
	} else if paths.DataDir != "" {
		paths.ConfigPath = filepath.Join(paths.DataDir, DefaultConfigName)
		migrateConfig(paths.ConfigPath, exeDir, cwd)
	} else {
		paths.ConfigPath = DefaultConfigName
	}
	return paths
}

// migrateConfig 首次运行时把历史 CWD 配置复制到数据目录（旧文件保留不删）。
func migrateConfig(target, exeDir, cwd string) {
	if _, err := os.Stat(target); err == nil {
		return
	}
	for _, dir := range []string{exeDir, cwd} {
		if dir == "" {
			continue
		}
		source := filepath.Join(dir, DefaultConfigName)
		if _, err := os.Stat(source); err != nil {
			continue
		}
		if err := copyFile(source, target); err != nil {
			return
		}
		return
	}
}

func executableDir() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(executable)
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s: %w", source, err)
	}
	return out.Sync()
}
