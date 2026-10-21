package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Runner 执行外部命令并返回合并输出（stdout+stderr）。
//
// 统一捕获输出是 GUI 可观测性的前提：GUI 产物没有控制台，
// 若直连 os.Stdout，脚本日志会彻底消失。CLI 拿到同一份输出后原样打印即可。
type Runner func(name string, args ...string) (string, error)

// DefaultRunner 执行子进程并捕获合并输出。
//
// 额外注入 PYTHONUTF8 / PYTHONIOENCODING：Python 脚本在 Windows 默认按
// cp936 输出中文，捕获后按 UTF-8 解码会变乱码，GUI 日志尤其明显。
func DefaultRunner(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "PYTHONUTF8=1", "PYTHONIOENCODING=utf-8")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// Service 是全部业务逻辑的唯一实现，CLI / HTTP / GUI 三个壳共享它。
type Service struct {
	Paths  Paths
	Runner Runner

	mu sync.Mutex
}

// New 构造 service；Runner 为空时使用真实子进程执行器。
func New(paths Paths) *Service {
	return &Service{
		Paths:  paths,
		Runner: DefaultRunner,
	}
}

// Apps 返回脚本目录中可调度的应用清单。
func (s *Service) Apps() ([]AppInfo, error) {
	return DiscoverApps(s.Paths.ScriptsDir)
}

// Config 读取当前生效的配置。
func (s *Service) Config() (Config, error) {
	return LoadConfig(s.Paths.ConfigPath)
}

// SetPreference 更新应用的款式或自定义图标（至少提供一项）。
func (s *Service) SetPreference(name, style, customIcon string) (Config, AppConfig, error) {
	if style == "" && customIcon == "" {
		return Config{}, AppConfig{}, newError("invalid_arguments", "需要 --style 或 --custom-icon 之一")
	}
	config, err := s.Config()
	if err != nil {
		return Config{}, AppConfig{}, newError("config_failed", "%v", err)
	}
	resolved := NormalizeAppName(name)
	preference := config.Apps[resolved]
	if style != "" {
		preference.Style = style
		preference.CustomIcon = ""
	}
	if customIcon != "" {
		preference.CustomIcon = customIcon
		preference.Style = ""
	}
	config.Apps[resolved] = preference
	if err := SaveConfig(s.Paths.ConfigPath, config); err != nil {
		return Config{}, AppConfig{}, newError("config_failed", "%v", err)
	}
	return config, preference, nil
}

// SetEnabled 切换单个应用的启用状态。
func (s *Service) SetEnabled(name string, enabled bool) (Config, AppConfig, error) {
	config, err := s.Config()
	if err != nil {
		return Config{}, AppConfig{}, newError("config_failed", "%v", err)
	}
	resolved := NormalizeAppName(name)
	preference := config.Apps[resolved]
	preference.Enabled = enabled
	config.Apps[resolved] = preference
	if err := SaveConfig(s.Paths.ConfigPath, config); err != nil {
		return Config{}, AppConfig{}, newError("config_failed", "%v", err)
	}
	return config, preference, nil
}

// Run 调度单个应用的补丁脚本，返回应用信息与脚本输出。
func (s *Service) Run(name string, options RunOptions) (AppInfo, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runLocked(name, options)
}

func (s *Service) runLocked(name string, options RunOptions) (AppInfo, string, error) {
	appName := NormalizeAppName(name)
	style := options.Style
	customIcon := options.CustomIcon
	if style == "" && customIcon == "" {
		config, err := LoadConfig(s.Paths.ConfigPath)
		if err != nil {
			return AppInfo{}, "", newError("config_failed", "%v", err)
		}
		if preference, ok := config.Apps[appName]; ok {
			style = preference.Style
			customIcon = preference.CustomIcon
		}
	}
	app, err := FindApp(s.Paths.ScriptsDir, appName)
	if err != nil {
		return AppInfo{}, "", err
	}
	if style != "" && !Contains(app.Styles, style) {
		return AppInfo{}, "", newError("style_not_found", "应用不支持款式: %s", style)
	}
	if app.Script == "" {
		return AppInfo{}, "", newError("script_not_found", "应用没有可执行补丁脚本")
	}

	args := []string{"run", app.Script}
	if style != "" {
		styleIcon := filepath.Join(s.Paths.ScriptsDir, appName, "custom_icons", style+".ico")
		if _, statErr := os.Stat(styleIcon); statErr == nil {
			args = append(args, "--custom-icon", styleIcon)
		} else {
			args = append(args, "--style", style)
		}
	}
	if customIcon != "" {
		args = append(args, "--custom-icon", customIcon)
	}
	if options.Kill {
		args = append(args, "--kill")
	}
	if options.RestartExplorer {
		args = append(args, "--restart-explorer")
	}
	output, err := s.Runner("uv", args...)
	if err != nil {
		return AppInfo{}, output, newError("script_failed", "%v", err)
	}
	return app, output, nil
}

// Revert 撤销指定应用的图标定制。
func (s *Service) Revert(name string, options RevertOptions) (AppInfo, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	appName := NormalizeAppName(name)
	app, err := FindApp(s.Paths.ScriptsDir, appName)
	if err != nil {
		return AppInfo{}, "", err
	}
	if app.Script == "" {
		return AppInfo{}, "", newError("script_not_found", "应用没有可执行补丁脚本")
	}
	args := []string{"run", app.Script, "--revert"}
	if options.RestartExplorer {
		args = append(args, "--restart-explorer")
	}
	output, err := s.Runner("uv", args...)
	if err != nil {
		return AppInfo{}, output, newError("script_failed", "%v", err)
	}
	return app, output, nil
}

// Apply 按配置批量打补丁；app 为空时处理全部 enabled 应用。
func (s *Service) Apply(options ApplyOptions) ([]string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	config, err := LoadConfig(s.Paths.ConfigPath)
	if err != nil {
		return nil, "", newError("config_failed", "%v", err)
	}
	names := make([]string, 0, len(config.Apps))
	if options.App != "" {
		names = append(names, NormalizeAppName(options.App))
	} else {
		for name, preference := range config.Apps {
			if preference.Enabled {
				names = append(names, name)
			}
		}
		sort.Strings(names)
	}

	applied := make([]string, 0, len(names))
	logs := make([]string, 0, len(names))
	for _, name := range names {
		preference, ok := config.Apps[name]
		if !ok {
			return applied, strings.Join(logs, "\n"), newError("app_not_configured", "配置中没有应用: %s", name)
		}
		if !preference.Enabled {
			continue
		}
		_, output, err := s.runLocked(name, RunOptions{
			Style:           preference.Style,
			CustomIcon:      preference.CustomIcon,
			Kill:            options.Kill,
			RestartExplorer: options.RestartExplorer,
		})
		if output != "" {
			logs = append(logs, output)
		}
		if err != nil {
			return applied, strings.Join(logs, "\n"), err
		}
		applied = append(applied, name)
	}
	return applied, strings.Join(logs, "\n"), nil
}

// TaskInstalled 查询登录守护计划任务是否已注册。
func (s *Service) TaskInstalled() bool {
	_, err := s.Runner("schtasks", "/Query", "/TN", ScheduledTask)
	return err == nil
}

// InstallTask 注册登录触发的计划任务，命令指向同目录的 console 通道产物。
func (s *Service) InstallTask() (string, error) {
	executable, err := s.cliExecutable()
	if err != nil {
		return "", newError("executable_failed", "%v", err)
	}
	target := fmt.Sprintf(`"%s" apply`, executable)
	// schtasks 在中文 Windows 输出 GBK，直接回显会乱码；只保留成败，不转发原始输出。
	if _, err := s.Runner("schtasks", "/Create", "/TN", ScheduledTask, "/TR", target, "/SC", "ONLOGON", "/F"); err != nil {
		return "", newError("task_failed", "%v", err)
	}
	return "", nil
}

// CLIPath 返回应当被计划任务调用的 console 通道产物路径。
func (s *Service) CLIPath() string {
	executable, err := s.cliExecutable()
	if err != nil {
		return ""
	}
	return executable
}

// cliExecutable 优先返回同目录的 taskbarguard-cli 产物，避免让计划任务指向 GUI 壳。
func (s *Service) cliExecutable() (string, error) {
	current, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(current)
	preferred := "taskbarguard-cli.exe"
	if s.Paths.Channel != "prod" {
		preferred = "taskbarguard-cli-dev.exe"
	}
	for _, name := range []string{preferred, "taskbarguard-cli.exe", "taskbarguard-cli-dev.exe"} {
		candidate := filepath.Join(dir, name)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return current, nil
}
