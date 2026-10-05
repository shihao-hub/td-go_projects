package main

/*
========================================================================================
TaskbarGuard - Windows 任务栏应用质感图标守卫与自动恢复引擎
========================================================================================

【项目定位】
面向 Windows 平台的任务栏图标统一美化、质感对齐与静默自愈工具。
解决 Electron / InnoSetup 应用（如 VS Code、DeepSeek Harness 等）在后台自动静默更新后，
PE 图标资源被官方默认图标覆盖导致任务栏视觉撕裂的痛点。

【当前实现】
1. 调度层 (Runner):
   - 默认扫描 `./scripts/<app>/` 目录。
   - 若指定 app 存在对应补丁脚本，通过 `uv run` 或嵌入式执行器调起执行。
   - 若找不到对应 app 目录或脚本，输出明确的规范报错。

2. 配置与偏好持久化 (Config Persistence):
   - 使用 config.yaml / config.json 记录各应用的激活款式（如 VS Code: blue vs silver）。
   - 用户在终端交互或通过参数选定后自动落盘。
   - 静默开机守护时无需交互，自动按偏好配置自愈。

3. 守卫与计划任务 (Task Scheduler Guard):
   - 管理 Windows 计划任务 (schtasks / Windows API)。
   - 在用户登录开机时极速静默校验应用 EXE 状态，发现被官方覆盖则秒级恢复。
   - 铁律：绝不 taskkill explorer.exe，仅广播 SHChangeNotify 安全刷新外壳。

4. 演进形态 (Future Roadmap):
   - 阶段 A: Go 驱动器 + 外部 Python scripts (uv run)
   - 阶段 B: Go //go:embed 编译全套 .ico 与注入工具，化为纯绿色免安装单文件二进制。
========================================================================================
*/

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	toolName       = "taskbarguard"
	schemaVersion  = "1.0.0"
	scheduledTask  = "TaskbarIconGuard"
	defaultScripts = "scripts"
)

type appInfo struct {
	Name   string   `json:"name"`
	Styles []string `json:"styles"`
	Script string   `json:"script"`
}

type output struct {
	OK   bool        `json:"ok"`
	Data interface{} `json:"data,omitempty"`
	Err  *cliError   `json:"error,omitempty"`
}

type cliError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var (
	version    = schemaVersion
	commitHash = "unknown"
)

var commandRunner = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Fprintln(stdout, version)
		return 0
	}
	if args[0] == "--schema" || args[0] == "schema" {
		return writeSchema(stdout)
	}

	switch args[0] {
	case "list":
		return listApps(args[1:], stdout, stderr)
	case "run":
		return runPatch(args[1:], stdout, stderr)
	case "config":
		return showConfig(args[1:], stdout, stderr)
	case "set":
		return setConfig(args[1:], stdout, stderr)
	case "apply":
		return applyConfig(args[1:], stdout, stderr)
	case "install-task":
		return installScheduledTask(args[1:], stdout, stderr)
	case "help", "--help", "-h":
		printHelp(stdout)
		return 0
	default:
		return reportError(stdout, stderr, false, "unknown_command", "未知命令: "+args[0])
	}
}

func listApps(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "输出 JSON")
	schemaOutput := fs.Bool("schema", false, "输出当前命令的 JSON 契约")
	scriptsDir := fs.String("scripts-dir", defaultScripts, "脚本目录")
	if err := fs.Parse(normalizeArgs(args)); err != nil {
		return 2
	}
	if *schemaOutput {
		return writeCommandSchema(stdout, "list")
	}
	apps, err := discoverApps(*scriptsDir)
	if err != nil {
		return reportError(stdout, stderr, *jsonOutput, "list_failed", err.Error())
	}
	if *jsonOutput {
		return writeJSON(stdout, output{OK: true, Data: apps})
	}
	for _, app := range apps {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", app.Name, strings.Join(app.Styles, ", "), app.Script)
	}
	return 0
}

func runPatch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "输出 JSON")
	schemaOutput := fs.Bool("schema", false, "输出当前命令的 JSON 契约")
	style := fs.String("style", "", "图标款式")
	customIcon := fs.String("custom-icon", "", "自定义 ICO 路径")
	configPath := fs.String("config", defaultConfigFile, "配置文件路径")
	scriptsDir := fs.String("scripts-dir", defaultScripts, "脚本目录")
	if err := fs.Parse(normalizeArgs(args)); err != nil {
		return 2
	}
	if *schemaOutput {
		return writeCommandSchema(stdout, "run")
	}
	positionals, err := parsePositionals(fs, args)
	if err != nil {
		return 2
	}
	if len(positionals) != 1 {
		return reportError(stdout, stderr, *jsonOutput, "invalid_arguments", "run 需要一个应用名")
	}
	appName := positionals[0]
	if *style == "" && *customIcon == "" {
		config, err := LoadConfig(*configPath)
		if err != nil {
			return reportError(stdout, stderr, *jsonOutput, "config_failed", err.Error())
		}
		if preference, ok := config.Apps[appName]; ok {
			*style = preference.Style
			*customIcon = preference.CustomIcon
		}
	}
	app, err := findApp(*scriptsDir, appName)
	if err != nil {
		return reportError(stdout, stderr, *jsonOutput, "app_not_found", err.Error())
	}
	if *style != "" && !contains(app.Styles, *style) {
		return reportError(stdout, stderr, *jsonOutput, "style_not_found", "应用不支持款式: "+*style)
	}
	if app.Script == "" {
		return reportError(stdout, stderr, *jsonOutput, "script_not_found", "应用没有可执行补丁脚本")
	}
	argsForScript := []string{app.Script}
	if *style != "" {
		customStylePath := filepath.Join(*scriptsDir, appName, "custom_icons", *style+".ico")
		if _, statErr := os.Stat(customStylePath); statErr == nil {
			argsForScript = append(argsForScript, "--custom-icon", customStylePath)
		} else {
			argsForScript = append(argsForScript, "--style", *style)
		}
	}
	if *customIcon != "" {
		argsForScript = append(argsForScript, "--custom-icon", *customIcon)
	}
	if err := commandRunner("uv", append([]string{"run"}, argsForScript...)...); err != nil {
		return reportError(stdout, stderr, *jsonOutput, "script_failed", err.Error())
	}
	if *jsonOutput {
		return writeJSON(stdout, output{OK: true, Data: map[string]string{"app": appName}})
	}
	fmt.Fprintf(stdout, "已完成 %s 补丁调度\n", appName)
	return 0
}

func installScheduledTask(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("install-task", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "输出 JSON")
	schemaOutput := fs.Bool("schema", false, "输出当前命令的 JSON 契约")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *schemaOutput {
		return writeCommandSchema(stdout, "install-task")
	}
	executable, err := os.Executable()
	if err != nil {
		return reportError(stdout, stderr, *jsonOutput, "executable_failed", err.Error())
	}
	if runtime.GOOS != "windows" {
		return reportError(stdout, stderr, *jsonOutput, "unsupported_platform", "计划任务仅支持 Windows")
	}
	cmdArgs := []string{"/Create", "/TN", scheduledTask, "/TR", fmt.Sprintf(`"%s" list`, executable), "/SC", "ONLOGON", "/F"}
	if err := commandRunner("schtasks", cmdArgs...); err != nil {
		return reportError(stdout, stderr, *jsonOutput, "task_failed", err.Error())
	}
	if *jsonOutput {
		return writeJSON(stdout, output{OK: true, Data: map[string]string{"task": scheduledTask}})
	}
	fmt.Fprintf(stdout, "已注册计划任务: %s\n", scheduledTask)
	return 0
}

func showConfig(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("file", defaultConfigFile, "配置文件路径")
	jsonOutput := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	config, err := LoadConfig(*configPath)
	if err != nil {
		return reportError(stdout, stderr, *jsonOutput, "config_failed", err.Error())
	}
	return writeJSON(stdout, config)
}

func setConfig(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", defaultConfigFile, "配置文件路径")
	style := fs.String("style", "", "预设款式")
	customIcon := fs.String("custom-icon", "", "自定义 ICO 路径")
	enabled := fs.Bool("enabled", true, "启用该应用")
	if err := fs.Parse(normalizeArgs(args)); err != nil {
		return 2
	}
	positionals, err := parsePositionals(fs, normalizeArgs(args))
	if err != nil {
		return 2
	}
	if len(positionals) != 1 || (*style == "" && *customIcon == "") {
		return reportError(stdout, stderr, false, "invalid_arguments", "用法: set APP (--style NAME | --custom-icon PATH) [--enabled=false]")
	}
	appName := positionals[0]
	config, err := LoadConfig(*configPath)
	if err != nil {
		return reportError(stdout, stderr, false, "config_failed", err.Error())
	}
	preference := config.Apps[appName]
	preference.Enabled = *enabled
	if *style != "" {
		preference.Style = *style
		preference.CustomIcon = ""
	}
	if *customIcon != "" {
		preference.CustomIcon = *customIcon
		preference.Style = ""
	}
	config.Apps[appName] = preference
	if err := SaveConfig(*configPath, config); err != nil {
		return reportError(stdout, stderr, false, "config_failed", err.Error())
	}
	return writeJSON(stdout, output{OK: true, Data: map[string]interface{}{"app": appName, "config": preference}})
}

func applyConfig(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", defaultConfigFile, "配置文件路径")
	scriptsDir := fs.String("scripts-dir", defaultScripts, "脚本目录")
	jsonOutput := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(normalizeArgs(args)); err != nil {
		return 2
	}
	positionals, err := parsePositionals(fs, normalizeArgs(args))
	if err != nil {
		return 2
	}
	if len(positionals) > 1 {
		return reportError(stdout, stderr, *jsonOutput, "invalid_arguments", "用法: apply [APP]")
	}
	config, err := LoadConfig(*configPath)
	if err != nil {
		return reportError(stdout, stderr, *jsonOutput, "config_failed", err.Error())
	}
	apps := make([]string, 0, len(config.Apps))
	if len(positionals) == 1 {
		apps = append(apps, positionals[0])
	} else {
		for name, preference := range config.Apps {
			if preference.Enabled {
				apps = append(apps, name)
			}
		}
		sort.Strings(apps)
	}
	applied := make([]string, 0, len(apps))
	for _, name := range apps {
		preference, ok := config.Apps[name]
		if !ok {
			return reportError(stdout, stderr, *jsonOutput, "app_not_configured", "配置中没有应用: "+name)
		}
		if !preference.Enabled {
			continue
		}
		command := []string{"run", "--config", *configPath, "--scripts-dir", *scriptsDir}
		if preference.Style != "" {
			command = append(command, "--style", preference.Style)
		}
		if preference.CustomIcon != "" {
			command = append(command, "--custom-icon", preference.CustomIcon)
		}
		command = append(command, name)
		childStdout := stdout
		if *jsonOutput {
			childStdout = io.Discard
		}
		if code := runPatch(command[1:], childStdout, stderr); code != 0 {
			return code
		}
		applied = append(applied, name)
	}
	if *jsonOutput {
		return writeJSON(stdout, output{OK: true, Data: map[string][]string{"applied": applied}})
	}
	return 0
}

func normalizeArgs(args []string) []string {
	valueFlags := map[string]bool{"--config": true, "--scripts-dir": true, "--style": true, "--custom-icon": true, "--file": true, "--enabled": true}
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "--") {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.SplitN(arg, "=", 2)[0]
		if valueFlags[name] && !strings.Contains(arg, "=") && index+1 < len(args) {
			index++
			flags = append(flags, args[index])
		}
	}
	return append(flags, positionals...)
}

func parsePositionals(fs *flag.FlagSet, args []string) ([]string, error) {
	positionals := append([]string(nil), fs.Args()...)
	if len(positionals) > 0 {
		return positionals, nil
	}
	knownFlags := map[string]bool{"--config": true, "--scripts-dir": true, "--style": true, "--custom-icon": true, "--file": true, "--json": true, "--schema": true, "--enabled": true}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if strings.HasPrefix(arg, "--") {
			name := strings.SplitN(arg, "=", 2)[0]
			if knownFlags[name] && !strings.Contains(arg, "=") && name != "--json" && name != "--schema" {
				index++
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	return positionals, nil
}

func discoverApps(scriptsDir string) ([]appInfo, error) {
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return nil, fmt.Errorf("无法读取脚本目录 %q: %w", scriptsDir, err)
	}
	apps := make([]appInfo, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		app, err := inspectApp(filepath.Join(scriptsDir, entry.Name()), entry.Name())
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

func findApp(scriptsDir, name string) (appInfo, error) {
	app, err := inspectApp(filepath.Join(scriptsDir, name), name)
	if err != nil {
		return appInfo{}, err
	}
	if app.Script == "" && len(app.Styles) == 0 {
		return appInfo{}, fmt.Errorf("未找到应用: %s", name)
	}
	return app, nil
}

func inspectApp(dir, name string) (appInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return appInfo{}, fmt.Errorf("未找到应用目录: %s", name)
		}
		return appInfo{}, err
	}
	app := appInfo{Name: name, Styles: []string{}}
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

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func writeSchema(stdout io.Writer) int {
	schema := map[string]interface{}{
		"name": toolName, "version": schemaVersion, "interface": "cli",
		"commands": []map[string]interface{}{
			{"name": "list", "schema": "taskbarguard list --schema"},
			{"name": "run", "schema": "taskbarguard run --schema"},
			{"name": "config", "usage": "taskbarguard config"},
			{"name": "set", "usage": "taskbarguard set APP --style NAME"},
			{"name": "apply", "usage": "taskbarguard apply [APP]"},
			{"name": "install-task", "schema": "taskbarguard install-task --schema"},
		},
	}
	return writeJSON(stdout, schema)
}

func writeCommandSchema(stdout io.Writer, command string) int {
	commands := map[string]interface{}{
		"list": map[string]interface{}{
			"name": "list", "interface": "cli",
			"arguments": map[string]interface{}{"positionals": 0},
			"options":   map[string]string{"--json": "boolean", "--schema": "boolean", "--scripts-dir": "string"},
			"output":    "{ok:true,data:appInfo[]}",
		},
		"run": map[string]interface{}{
			"name": "run", "interface": "cli",
			"arguments": map[string]interface{}{"positionals": 1, "positionalName": "app"},
			"options":   map[string]string{"--json": "boolean", "--schema": "boolean", "--style": "string", "--custom-icon": "path", "--scripts-dir": "string"},
			"output":    "{ok:true,data:{app:string}}",
		},
		"config": map[string]interface{}{
			"name": "config", "interface": "cli", "arguments": map[string]interface{}{"positionals": 0},
			"options": map[string]string{"--file": "path", "--json": "boolean"}, "output": "Config",
		},
		"set": map[string]interface{}{
			"name": "set", "interface": "cli", "arguments": map[string]interface{}{"positionals": 1, "positionalName": "app"},
			"options": map[string]string{"--config": "path", "--style": "string", "--custom-icon": "path", "--enabled": "boolean"}, "output": "{ok:true,data:{app:string,config:AppConfig}}",
		},
		"apply": map[string]interface{}{
			"name": "apply", "interface": "cli", "arguments": map[string]interface{}{"positionals": "0..1", "positionalName": "app"},
			"options": map[string]string{"--config": "path", "--scripts-dir": "path", "--json": "boolean"}, "output": "{ok:true,data:{applied:string[]}}",
		},
		"install-task": map[string]interface{}{
			"name": "install-task", "interface": "cli",
			"arguments": map[string]interface{}{"positionals": 0},
			"options":   map[string]string{"--json": "boolean", "--schema": "boolean"},
			"output":    "{ok:true,data:{task:string}}",
		},
	}
	definition, ok := commands[command]
	if !ok {
		return reportError(stdout, io.Discard, false, "unknown_command", "未知命令: "+command)
	}
	return writeJSON(stdout, definition)
}

func writeJSON(w io.Writer, value interface{}) int {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return 1
	}
	return 0
}

func reportError(stdout, stderr io.Writer, jsonOutput bool, code, message string) int {
	if jsonOutput {
		_ = writeJSON(stdout, output{OK: false, Err: &cliError{Code: code, Message: message}})
	} else {
		fmt.Fprintln(stderr, message)
	}
	return 1
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "TaskbarGuard - Windows 任务栏应用质感图标守卫")
	fmt.Fprintln(w, "用法: taskbarguard <list|run|config|set|apply|install-task|schema>")
	fmt.Fprintln(w, "  list          列出脚本目录中的支持应用")
	fmt.Fprintln(w, "  run APP       调度指定应用的补丁脚本（未传 --style 时回退读取配置）")
	fmt.Fprintln(w, "  config        打印当前生效的配置 (config.json)")
	fmt.Fprintln(w, "  set APP       更新偏好并自动落盘: --style NAME | --custom-icon PATH | --enabled=false")
	fmt.Fprintln(w, "  apply [APP]   按配置一键打补丁，缺省应用所有已启用应用")
	fmt.Fprintln(w, "  install-task  注册用户登录时的守护计划任务")
	fmt.Fprintln(w, "  schema        输出 CLI 契约 JSON")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "构建: python build.py --dev | python build.py --release")
}
