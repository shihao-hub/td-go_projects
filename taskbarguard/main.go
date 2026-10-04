package main

/*
========================================================================================
TaskbarGuard - Windows 任务栏应用质感图标守卫与自动恢复引擎
========================================================================================

【项目定位】
面向 Windows 平台的任务栏图标统一美化、质感对齐与静默自愈工具。
解决 Electron / InnoSetup 应用（如 VS Code、DeepSeek Harness 等）在后台自动静默更新后，
PE 图标资源被官方默认图标覆盖导致任务栏视觉撕裂的痛点。

【架构设计蓝图 (待实现)】
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
		fmt.Fprintln(stdout, schemaVersion)
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
	scriptsDir := fs.String("scripts-dir", defaultScripts, "脚本目录")
	if err := fs.Parse(args); err != nil {
		return 2
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
	style := fs.String("style", "", "图标款式")
	customIcon := fs.String("custom-icon", "", "自定义 ICO 路径")
	scriptsDir := fs.String("scripts-dir", defaultScripts, "脚本目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		return reportError(stdout, stderr, *jsonOutput, "invalid_arguments", "run 需要一个应用名")
	}
	appName := fs.Arg(0)
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
		argsForScript = append(argsForScript, "--style", *style)
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
	executable, err := os.Executable()
	if err != nil {
		return reportError(stdout, stderr, *jsonOutput, "executable_failed", err.Error())
	}
	if err := fs.Parse(args); err != nil {
		return 2
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
			{"name": "list", "options": []string{"--json", "--scripts-dir"}},
			{"name": "run", "options": []string{"--style", "--custom-icon", "--json", "--scripts-dir"}},
			{"name": "install-task", "options": []string{"--json"}},
		},
	}
	return writeJSON(stdout, schema)
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
	fmt.Fprintln(w, "用法: taskbarguard <list|run|install-task|schema>")
	fmt.Fprintln(w, "  list          列出脚本目录中的支持应用")
	fmt.Fprintln(w, "  run APP       调度指定应用的补丁脚本")
	fmt.Fprintln(w, "  install-task  注册用户登录时的守护计划任务")
	fmt.Fprintln(w, "  schema        输出 CLI 契约 JSON")
}
