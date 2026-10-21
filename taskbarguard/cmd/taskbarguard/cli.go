//go:build cli

// Console 通道入口（taskbarguard-cli）：终端 / 脚本 / Agent 专用。
//
// 该产物为 console 子系统，父 shell 会正常等待进程退出，prompt、光标、
// 管道与输出重定向全部符合预期。GUI 能力请用 taskbarguard-gui.exe。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"taskbarguard/internal/api"
	"taskbarguard/internal/buildinfo"
	"taskbarguard/internal/core"
)

type output struct {
	OK   bool        `json:"ok"`
	Data interface{} `json:"data,omitempty"`
	Err  *cliError   `json:"error,omitempty"`
}

type cliError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func newService(configPath, scriptsDir string) *core.Service {
	return core.New(core.ResolvePaths(buildinfo.Channel(), scriptsDir, configPath))
}

func serviceMeta(service *core.Service) api.Meta {
	return api.Meta{
		Channel:    buildinfo.Channel(),
		Version:    buildinfo.Version,
		BuildMode:  buildinfo.BuildMode,
		ConfigPath: service.Paths.ConfigPath,
		ScriptsDir: service.Paths.ScriptsDir,
		DataDir:    service.Paths.DataDir,
		TaskName:   core.ScheduledTask,
	}
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Fprintf(stdout, "%s (mode: %s, channel: %s)\n", buildinfo.Version, buildinfo.BuildMode, buildinfo.Channel())
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
	case "revert":
		return revertPatch(args[1:], stdout, stderr)
	case "config":
		return showConfig(args[1:], stdout, stderr)
	case "set":
		return setConfig(args[1:], stdout, stderr)
	case "apply":
		return applyConfig(args[1:], stdout, stderr)
	case "install-task":
		return installScheduledTask(args[1:], stdout, stderr)
	case "serve":
		return serve(args[1:], stdout, stderr)
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
	scriptsDir := fs.String("scripts-dir", "", "脚本目录（默认自动解析）")
	if err := fs.Parse(normalizeArgs(args)); err != nil {
		return 2
	}
	if *schemaOutput {
		return writeCommandSchema(stdout, "list")
	}
	service := newService("", *scriptsDir)
	apps, err := service.Apps()
	if err != nil {
		return reportCoreError(stdout, stderr, *jsonOutput, err)
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
	kill := fs.Bool("kill", false, "强制关闭正在运行的目标应用")
	restartExplorer := fs.Bool("restart-explorer", false, "彻底重构图标缓存并重启 Explorer")
	configPath := fs.String("config", "", "配置文件路径（默认自动解析）")
	scriptsDir := fs.String("scripts-dir", "", "脚本目录（默认自动解析）")
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
	service := newService(*configPath, *scriptsDir)
	appName := core.NormalizeAppName(positionals[0])
	_, scriptOutput, err := service.Run(appName, core.RunOptions{
		Style:           *style,
		CustomIcon:      *customIcon,
		Kill:            *kill,
		RestartExplorer: *restartExplorer,
	})
	if err != nil {
		return reportCoreError(stdout, stderr, *jsonOutput, err)
	}
	if scriptOutput != "" && !*jsonOutput {
		fmt.Fprint(stdout, scriptOutput)
	}
	if *jsonOutput {
		return writeJSON(stdout, output{OK: true, Data: map[string]string{"app": appName}})
	}
	fmt.Fprintf(stdout, "已完成 %s 补丁调度\n", appName)
	return 0
}

func revertPatch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("revert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "输出 JSON")
	schemaOutput := fs.Bool("schema", false, "输出当前命令的 JSON 契约")
	restartExplorer := fs.Bool("restart-explorer", false, "彻底重构图标缓存并重启 Explorer")
	configPath := fs.String("config", "", "配置文件路径（默认自动解析）")
	scriptsDir := fs.String("scripts-dir", "", "脚本目录（默认自动解析）")
	if err := fs.Parse(normalizeArgs(args)); err != nil {
		return 2
	}
	if *schemaOutput {
		return writeCommandSchema(stdout, "revert")
	}
	positionals, err := parsePositionals(fs, args)
	if err != nil {
		return 2
	}
	if len(positionals) != 1 {
		return reportError(stdout, stderr, *jsonOutput, "invalid_arguments", "revert 需要一个应用名")
	}
	service := newService(*configPath, *scriptsDir)
	appName := core.NormalizeAppName(positionals[0])
	_, scriptOutput, err := service.Revert(appName, core.RevertOptions{RestartExplorer: *restartExplorer})
	if err != nil {
		return reportCoreError(stdout, stderr, *jsonOutput, err)
	}
	if scriptOutput != "" && !*jsonOutput {
		fmt.Fprint(stdout, scriptOutput)
	}
	if *jsonOutput {
		return writeJSON(stdout, output{OK: true, Data: map[string]string{"reverted": appName}})
	}
	fmt.Fprintf(stdout, "已完成 %s 图标回退与恢复调度\n", appName)
	return 0
}

func installScheduledTask(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("install-task", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "输出 JSON")
	schemaOutput := fs.Bool("schema", false, "输出当前命令的 JSON 契约")
	configPath := fs.String("config", "", "配置文件路径（默认自动解析）")
	scriptsDir := fs.String("scripts-dir", "", "脚本目录（默认自动解析）")
	if err := fs.Parse(normalizeArgs(args)); err != nil {
		return 2
	}
	if *schemaOutput {
		return writeCommandSchema(stdout, "install-task")
	}
	if runtime.GOOS != "windows" {
		return reportError(stdout, stderr, *jsonOutput, "unsupported_platform", "计划任务仅支持 Windows")
	}
	service := newService(*configPath, *scriptsDir)
	if strings.Contains(strings.ToLower(filepath.Base(service.CLIPath())), "-dev") {
		fmt.Fprintln(stderr, "警告: 当前为开发版可执行文件，注册的任务将指向开发版；正式启用前请改用 release 版重新注册。")
	}
	taskOutput, err := service.InstallTask()
	if err != nil {
		if taskOutput != "" {
			fmt.Fprint(stderr, taskOutput)
		}
		return reportCoreError(stdout, stderr, *jsonOutput, err)
	}
	if *jsonOutput {
		return writeJSON(stdout, output{OK: true, Data: map[string]string{"task": core.ScheduledTask}})
	}
	if taskOutput != "" {
		fmt.Fprint(stdout, taskOutput)
	}
	fmt.Fprintf(stdout, "已注册计划任务: %s\n", core.ScheduledTask)
	return 0
}

func showConfig(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("file", "", "配置文件路径（默认自动解析）")
	jsonOutput := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	service := newService(*configPath, "")
	config, err := service.Config()
	if err != nil {
		return reportCoreError(stdout, stderr, *jsonOutput, err)
	}
	return writeJSON(stdout, config)
}

func setConfig(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "配置文件路径（默认自动解析）")
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
	service := newService(*configPath, "")
	appName := core.NormalizeAppName(positionals[0])
	if _, _, err := service.SetPreference(appName, *style, *customIcon); err != nil {
		return reportCoreError(stdout, stderr, false, err)
	}
	_, preference, err := service.SetEnabled(appName, *enabled)
	if err != nil {
		return reportCoreError(stdout, stderr, false, err)
	}
	return writeJSON(stdout, output{OK: true, Data: map[string]interface{}{"app": appName, "config": preference}})
}

func applyConfig(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "配置文件路径（默认自动解析）")
	scriptsDir := fs.String("scripts-dir", "", "脚本目录（默认自动解析）")
	jsonOutput := fs.Bool("json", false, "输出 JSON")
	kill := fs.Bool("kill", false, "强制关闭正在运行的目标应用")
	restartExplorer := fs.Bool("restart-explorer", false, "彻底重构图标缓存并重启 Explorer")
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
	appName := ""
	if len(positionals) == 1 {
		appName = core.NormalizeAppName(positionals[0])
	}
	service := newService(*configPath, *scriptsDir)
	applied, scriptOutput, err := service.Apply(core.ApplyOptions{
		App:             appName,
		Kill:            *kill,
		RestartExplorer: *restartExplorer,
	})
	if err != nil {
		if scriptOutput != "" && !*jsonOutput {
			fmt.Fprint(stdout, scriptOutput)
		}
		return reportCoreError(stdout, stderr, *jsonOutput, err)
	}
	if scriptOutput != "" && !*jsonOutput {
		fmt.Fprint(stdout, scriptOutput)
	}
	if *jsonOutput {
		return writeJSON(stdout, output{OK: true, Data: map[string][]string{"applied": applied}})
	}
	return 0
}

func serve(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", 0, "监听端口（0 为动态探测）")
	openBrowser := fs.Bool("open", false, "启动后自动打开浏览器")
	schemaOutput := fs.Bool("schema", false, "输出当前命令的 JSON 契约")
	configPath := fs.String("config", "", "配置文件路径（默认自动解析）")
	scriptsDir := fs.String("scripts-dir", "", "脚本目录（默认自动解析）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *schemaOutput {
		return writeCommandSchema(stdout, "serve")
	}
	service := newService(*configPath, *scriptsDir)
	listener, err := api.Listen(*port)
	if err != nil {
		return reportError(stdout, stderr, false, "listen_failed", err.Error())
	}
	url := api.URL(listener)
	fmt.Fprintf(stdout, "%s WebUI is running at %s\n", core.ToolName, url)
	if *openBrowser {
		_ = launchBrowser(url)
	}
	server := api.NewServer(service, serviceMeta(service))
	if err := server.Serve(listener); err != nil {
		return reportError(stdout, stderr, false, "serve_failed", err.Error())
	}
	return 0
}

func launchBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
	default:
		cmd = "xdg-open"
	}
	return exec.Command(cmd, args...).Start()
}

func normalizeArgs(args []string) []string {
	valueFlags := map[string]bool{"--config": true, "--scripts-dir": true, "--style": true, "--custom-icon": true, "--file": true, "--enabled": true, "--port": true}
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
	knownFlags := map[string]bool{
		"--config": true, "--scripts-dir": true, "--style": true, "--custom-icon": true,
		"--file": true, "--json": true, "--schema": true, "--enabled": true,
		"--kill": true, "--restart-explorer": true, "--port": true, "--open": true,
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if strings.HasPrefix(arg, "--") {
			name := strings.SplitN(arg, "=", 2)[0]
			if knownFlags[name] && !strings.Contains(arg, "=") && name != "--json" && name != "--schema" && name != "--kill" && name != "--restart-explorer" && name != "--open" {
				index++
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	return positionals, nil
}

func writeSchema(stdout io.Writer) int {
	schema := map[string]interface{}{
		"name": core.ToolName, "version": core.SchemaVersion, "interface": "cli",
		"commands": []map[string]interface{}{
			{"name": "list", "schema": "taskbarguard-cli list --schema"},
			{"name": "run", "schema": "taskbarguard-cli run --schema"},
			{"name": "revert", "schema": "taskbarguard-cli revert --schema"},
			{"name": "config", "usage": "taskbarguard-cli config"},
			{"name": "set", "usage": "taskbarguard-cli set APP --style NAME"},
			{"name": "apply", "usage": "taskbarguard-cli apply [APP]"},
			{"name": "install-task", "schema": "taskbarguard-cli install-task --schema"},
			{"name": "serve", "schema": "taskbarguard-cli serve --schema"},
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
			"options":   map[string]string{"--json": "boolean", "--schema": "boolean", "--style": "string", "--custom-icon": "path", "--config": "path", "--scripts-dir": "string", "--kill": "boolean", "--restart-explorer": "boolean"},
			"output":    "{ok:true,data:{app:string}}",
		},
		"revert": map[string]interface{}{
			"name": "revert", "interface": "cli",
			"arguments": map[string]interface{}{"positionals": 1, "positionalName": "app"},
			"options":   map[string]string{"--json": "boolean", "--schema": "boolean", "--config": "path", "--scripts-dir": "string", "--restart-explorer": "boolean"},
			"output":    "{ok:true,data:{reverted:string}}",
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
			"options": map[string]string{"--config": "path", "--scripts-dir": "path", "--json": "boolean", "--kill": "boolean", "--restart-explorer": "boolean"}, "output": "{ok:true,data:{applied:string[]}}",
		},
		"install-task": map[string]interface{}{
			"name": "install-task", "interface": "cli",
			"arguments": map[string]interface{}{"positionals": 0},
			"options":   map[string]string{"--json": "boolean", "--schema": "boolean", "--config": "path", "--scripts-dir": "path"},
			"output":    "{ok:true,data:{task:string}}",
		},
		"serve": map[string]interface{}{
			"name": "serve", "interface": "cli", "blocking": true,
			"arguments": map[string]interface{}{"positionals": 0},
			"options":   map[string]string{"--port": "int", "--open": "boolean", "--config": "path", "--scripts-dir": "path"},
			"output":    "WebUI at http://127.0.0.1:<port>",
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

func reportCoreError(stdout, stderr io.Writer, jsonOutput bool, err error) int {
	return reportError(stdout, stderr, jsonOutput, core.ErrorCode(err), err.Error())
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
	fmt.Fprintln(w, "用法: taskbarguard-cli <list|run|revert|config|set|apply|install-task|serve|schema>")
	fmt.Fprintln(w, "  list          列出脚本目录中的支持应用")
	fmt.Fprintln(w, "  run APP       调度补丁脚本（--kill 强制关闭占用进程后写入，--restart-explorer 彻底重构图标缓存）")
	fmt.Fprintln(w, "  revert APP    撤销指定应用的图标定制并恢复原生基线状态（--restart-explorer 彻底刷新）")
	fmt.Fprintln(w, "  config        打印当前生效的配置")
	fmt.Fprintln(w, "  set APP       更新偏好并自动落盘: --style NAME | --custom-icon PATH | --enabled=false")
	fmt.Fprintln(w, "  apply [APP]   按配置一键打补丁（缺省全部已启用应用）")
	fmt.Fprintln(w, "  install-task  注册用户登录时的守护计划任务（指向 taskbarguard-cli）")
	fmt.Fprintln(w, "  serve         启动内嵌 WebUI 工作台（--port 0 动态端口，--open 自动开浏览器）")
	fmt.Fprintln(w, "  schema        输出 CLI 契约 JSON")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "通道: taskbarguard-cli = 本程序（终端）；taskbarguard-gui = 桌面窗口（不接受子命令）")
	fmt.Fprintln(w, "构建: uv run scripts/build.py [--release]")
}
