// Command rebootremind 是 Windows 开机时长与内存健康巡检重启提醒工具。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"rebootremind/internal/buildinfo"
	"rebootremind/internal/config"
	"rebootremind/internal/fsutil"
	"rebootremind/internal/model"
	"rebootremind/internal/scheduler"
	"rebootremind/internal/service"
	"rebootremind/internal/sysactions"
	"rebootremind/internal/sysinfo"
	"rebootremind/internal/ui"
)

const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

const reasonAnotherInstance = "another_instance"

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *errorBody `json:"error,omitempty"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return exitOK
	}
	switch args[0] {
	case "status":
		return runStatus(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "task":
		return runTask(args[1:], stdout, stderr)
	case "config":
		return runConfig(args[1:], stdout, stderr)
	case "schema":
		return runSchema(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		return runVersion(args[1:], stdout, stderr)
	case "help", "--help", "-h":
		printHelp(stdout)
		return exitOK
	default:
		fmt.Fprintf(stderr, "未知命令: %s\n\n", args[0])
		printHelp(stderr)
		return exitUsage
	}
}

func runStatus(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("status", stderr)
	jsonMode := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		return fail(stdout, stderr, *jsonMode, exitUsage, "invalid_argument", "status 不接受位置参数")
	}

	cfg, err := config.Load()
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "config_error", err.Error())
	}
	dir, err := config.DataDir()
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "io_error", err.Error())
	}
	state, err := service.LoadState(dir)
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "state_error", err.Error())
	}
	snapshot, err := sysinfo.Collect(5)
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "sysinfo_error", err.Error())
	}

	now := time.Now()
	decision := service.Evaluate(snapshot, cfg, state.SnoozeUntil, now)
	status := model.Status{
		Snapshot:     snapshot,
		Thresholds:   thresholds(cfg),
		SnoozeUntil:  state.SnoozeUntil,
		SnoozeActive: state.SnoozeUntil != nil && now.Before(*state.SnoozeUntil),
		WouldNotify:  decision.ShouldNotify,
		Health:       service.Health(snapshot, cfg),
	}

	if *jsonMode {
		return emit(stdout, stderr, status)
	}
	printStatus(stdout, status, decision)
	return exitOK
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("check", stderr)
	notify := fs.Bool("notify", false, "命中规则时弹出提醒弹窗")
	forceFlag := fs.Bool("force", false, "跳过阈值判定强制提醒（用于验证）")
	jsonMode := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		return fail(stdout, stderr, *jsonMode, exitUsage, "invalid_argument", "check 不接受位置参数")
	}

	cfg, err := config.Load()
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "config_error", err.Error())
	}
	dir, err := config.DataDir()
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "io_error", err.Error())
	}
	state, err := service.LoadState(dir)
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "state_error", err.Error())
	}
	snapshot, err := sysinfo.Collect(5)
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "sysinfo_error", err.Error())
	}

	now := time.Now()
	decision := service.Evaluate(snapshot, cfg, state.SnoozeUntil, now)
	if *forceFlag {
		decision = model.Decision{
			ShouldNotify: true,
			Trigger:      model.TriggerForced,
			Reason:       model.ReasonForced,
		}
	}

	result := model.CheckResult{
		Forced:      *forceFlag,
		Decision:    decision,
		Snapshot:    snapshot,
		SnoozeUntil: state.SnoozeUntil,
	}

	if decision.ShouldNotify && *notify {
		lock, lockErr := fsutil.AcquireLock(filepath.Join(dir, "check.lock"), 2*time.Minute)
		switch {
		case errors.Is(lockErr, fsutil.ErrLocked):
			// 另一个巡检进程正在处理，直接退避，不重复弹窗。
			result.Decision = model.Decision{ShouldNotify: false, Reason: reasonAnotherInstance}
		case lockErr != nil:
			return fail(stdout, stderr, *jsonMode, exitFail, "io_error", lockErr.Error())
		default:
			defer lock.Release()
			// 持锁后重新读取状态，避免与已完成的提醒动作竞态。
			state, err = service.LoadState(dir)
			if err != nil {
				return fail(stdout, stderr, *jsonMode, exitFail, "state_error", err.Error())
			}
			if !*forceFlag {
				decision = service.Evaluate(snapshot, cfg, state.SnoozeUntil, now)
				result.Decision = decision
			}
			if decision.ShouldNotify {
				action, dialogErr := ui.ShowReminder(snapshot)
				if dialogErr != nil {
					return fail(stdout, stderr, *jsonMode, exitFail, "dialog_error", dialogErr.Error())
				}
				restartRequested, actionName, actionErr := applyAction(dir, &state, cfg, action, now)
				if actionErr != nil {
					return fail(stdout, stderr, *jsonMode, exitFail, "action_error", actionErr.Error())
				}
				result.Notified = true
				result.RestartRequested = restartRequested
				result.Action = actionName
				result.SnoozeUntil = state.SnoozeUntil
			}
		}
	}

	if *jsonMode {
		return emit(stdout, stderr, result)
	}
	if result.Notified {
		if result.RestartRequested {
			fmt.Fprintln(stdout, "已请求立即重启计算机。")
		} else {
			fmt.Fprintf(stdout, "已记录提醒动作：%s\n", actionLabel(result.Action))
		}
	} else if !*notify {
		fmt.Fprintf(stdout, "巡检完成：%s（%s）\n", notifyLabel(result.Decision.ShouldNotify), reasonLabel(result.Decision.Reason))
	}
	return exitOK
}

func runTask(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "用法: rebootremind task <install|uninstall|status> [选项]")
		return exitUsage
	}
	switch args[0] {
	case "install":
		return runTaskInstall(args[1:], stdout, stderr)
	case "uninstall":
		return runTaskUninstall(args[1:], stdout, stderr)
	case "status":
		return runTaskStatus(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知 task 子命令: %s\n", args[0])
		return exitUsage
	}
}

func runTaskInstall(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("task install", stderr)
	forceFlag := fs.Bool("force", false, "允许注册来自临时构建路径的 exe")
	jsonMode := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		return fail(stdout, stderr, *jsonMode, exitUsage, "invalid_argument", "task install 不接受位置参数")
	}

	exePath, err := os.Executable()
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "io_error", err.Error())
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "io_error", err.Error())
	}
	if scheduler.IsUnstablePath(exePath) && !*forceFlag {
		return fail(stdout, stderr, *jsonMode, exitFail, "unstable_executable",
			"当前 exe 来自临时构建目录，无法注册稳定的计划任务；请先运行 uv run scripts/build.py --dev，再用 bin/rebootremind-dev.exe task install（或加 --force 强行注册）")
	}
	if err := scheduler.Install(scheduler.DefaultTaskName, exePath); err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "task_error", err.Error())
	}

	data := model.TaskStatus{Name: scheduler.DefaultTaskName, Installed: true, ExePath: exePath}
	if *jsonMode {
		return emit(stdout, stderr, data)
	}
	fmt.Fprintf(stdout, "已安装计划任务 %s（每小时运行一次）\n", data.Name)
	fmt.Fprintf(stdout, "  exe: %s\n", data.ExePath)
	fmt.Fprintf(stdout, "  卸载: rebootremind task uninstall\n")
	return exitOK
}

func runTaskUninstall(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("task uninstall", stderr)
	jsonMode := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		return fail(stdout, stderr, *jsonMode, exitUsage, "invalid_argument", "task uninstall 不接受位置参数")
	}
	removed, err := scheduler.Uninstall(scheduler.DefaultTaskName)
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "task_error", err.Error())
	}
	exePath, _ := os.Executable()
	data := model.TaskStatus{Name: scheduler.DefaultTaskName, Installed: false, ExePath: exePath}
	if *jsonMode {
		return emit(stdout, stderr, data)
	}
	if removed {
		fmt.Fprintf(stdout, "已卸载计划任务 %s\n", scheduler.DefaultTaskName)
	} else {
		fmt.Fprintf(stdout, "计划任务 %s 未安装，无需卸载\n", scheduler.DefaultTaskName)
	}
	return exitOK
}

func runTaskStatus(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("task status", stderr)
	jsonMode := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		return fail(stdout, stderr, *jsonMode, exitUsage, "invalid_argument", "task status 不接受位置参数")
	}
	installed, detail, err := scheduler.Query(scheduler.DefaultTaskName)
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "task_error", err.Error())
	}
	exePath, _ := os.Executable()
	data := model.TaskStatus{
		Name:      scheduler.DefaultTaskName,
		Installed: installed,
		ExePath:   exePath,
		NextRun:   scheduler.NextRun(detail),
	}
	if *jsonMode {
		return emit(stdout, stderr, data)
	}
	if installed {
		fmt.Fprintf(stdout, "计划任务 %s 已安装\n", data.Name)
		if data.NextRun != "" {
			fmt.Fprintf(stdout, "  下次运行: %s\n", data.NextRun)
		}
	} else {
		fmt.Fprintf(stdout, "计划任务 %s 未安装\n", data.Name)
	}
	return exitOK
}

func runConfig(args []string, stdout, stderr io.Writer) int {
	sub := "show"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		rest = args[1:]
	}
	switch sub {
	case "show":
		return runConfigShow(rest, stdout, stderr)
	case "set":
		return runConfigSet(rest, stdout, stderr)
	case "path":
		return runConfigPath(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知 config 子命令: %s\n", sub)
		return exitUsage
	}
}

func runConfigShow(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("config show", stderr)
	jsonMode := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		return fail(stdout, stderr, *jsonMode, exitUsage, "invalid_argument", "config show 不接受位置参数")
	}
	cfg, err := config.Load()
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "config_error", err.Error())
	}
	if *jsonMode {
		return emit(stdout, stderr, cfg)
	}
	dir, _ := config.DataDir()
	fmt.Fprintf(stdout, "配置（%s）\n", filepath.Join(dir, "config.json"))
	fmt.Fprintf(stdout, "  min_uptime_hours    %g\n", cfg.MinUptimeHours)
	fmt.Fprintf(stdout, "  min_memory_percent  %g\n", cfg.MinMemoryPercent)
	fmt.Fprintf(stdout, "  force_uptime_hours  %g\n", cfg.ForceUptimeHours)
	fmt.Fprintf(stdout, "  snooze_short_hours  %g\n", cfg.SnoozeShortHours)
	fmt.Fprintf(stdout, "  snooze_long_hours   %g\n", cfg.SnoozeLongHours)
	return exitOK
}

func runConfigSet(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("config set", stderr)
	jsonMode := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 2 {
		return fail(stdout, stderr, *jsonMode, exitUsage, "invalid_argument", "用法: rebootremind config set <key> <value>")
	}
	key := fs.Arg(0)
	value, err := strconv.ParseFloat(fs.Arg(1), 64)
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitUsage, "invalid_argument", "value 必须是数字: "+fs.Arg(1))
	}

	cfg, err := config.Load()
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "config_error", err.Error())
	}
	switch key {
	case "min_uptime_hours":
		cfg.MinUptimeHours = value
	case "min_memory_percent":
		cfg.MinMemoryPercent = value
	case "force_uptime_hours":
		cfg.ForceUptimeHours = value
	case "snooze_short_hours":
		cfg.SnoozeShortHours = value
	case "snooze_long_hours":
		cfg.SnoozeLongHours = value
	default:
		return fail(stdout, stderr, *jsonMode, exitUsage, "unknown_key",
			"未知配置项: "+key+"（可选：min_uptime_hours、min_memory_percent、force_uptime_hours、snooze_short_hours、snooze_long_hours）")
	}
	if err := cfg.Validate(); err != nil {
		return fail(stdout, stderr, *jsonMode, exitUsage, "invalid_argument", err.Error())
	}
	if err := config.Save(cfg); err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "io_error", err.Error())
	}
	if *jsonMode {
		return emit(stdout, stderr, cfg)
	}
	fmt.Fprintf(stdout, "已更新 %s = %g\n", key, value)
	return exitOK
}

func runConfigPath(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("config path", stderr)
	jsonMode := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		return fail(stdout, stderr, *jsonMode, exitUsage, "invalid_argument", "config path 不接受位置参数")
	}
	dir, err := config.DataDir()
	if err != nil {
		return fail(stdout, stderr, *jsonMode, exitFail, "io_error", err.Error())
	}
	if *jsonMode {
		return emit(stdout, stderr, map[string]string{"data_dir": dir})
	}
	fmt.Fprintln(stdout, dir)
	return exitOK
}

func runSchema(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "用法: rebootremind schema")
		return exitUsage
	}
	if err := emitJSON(stdout, model.SchemaDoc(buildinfo.DisplayVersion())); err != nil {
		fmt.Fprintf(stderr, "输出 schema 失败: %v\n", err)
		return exitFail
	}
	return exitOK
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("version", stderr)
	jsonMode := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		return fail(stdout, stderr, *jsonMode, exitUsage, "invalid_argument", "version 不接受位置参数")
	}
	data := map[string]string{
		"version":  buildinfo.Version,
		"mode":     buildinfo.Mode,
		"channel":  buildinfo.Channel(),
		"build_id": buildinfo.BuildID,
	}
	if *jsonMode {
		return emit(stdout, stderr, data)
	}
	fmt.Fprintf(stdout, "rebootremind %s\n", buildinfo.DisplayVersion())
	fmt.Fprintf(stdout, "  构建通道: %s（数据目录 %s）\n", buildinfo.Mode, buildinfo.Channel())
	return exitOK
}

func applyAction(dir string, state *service.State, cfg config.Config, action ui.Action, now time.Time) (bool, string, error) {
	switch action {
	case ui.ActionSnoozeShort:
		service.Snooze(state, now, durationHours(cfg.SnoozeShortHours), service.ActionSnoozeShort)
	case ui.ActionSnoozeLong:
		service.Snooze(state, now, durationHours(cfg.SnoozeLongHours), service.ActionSnoozeLong)
	case ui.ActionRestart:
		confirmed, err := ui.ConfirmRestart()
		if err != nil {
			return false, "", err
		}
		if !confirmed {
			service.Snooze(state, now, durationHours(cfg.SnoozeShortHours), service.ActionDismissed)
			state.LastNotifiedAt = &now
			if err := service.SaveState(dir, *state); err != nil {
				return false, "", err
			}
			return false, "restart_cancelled", nil
		}
		state.LastNotifiedAt = &now
		state.LastAction = service.ActionRestart
		state.SnoozeUntil = nil
		if err := service.SaveState(dir, *state); err != nil {
			return false, "", err
		}
		if err := sysactions.Restart(); err != nil {
			return false, "", err
		}
		return true, service.ActionRestart, nil
	default:
		service.Snooze(state, now, durationHours(cfg.SnoozeShortHours), service.ActionDismissed)
	}

	state.LastNotifiedAt = &now
	if err := service.SaveState(dir, *state); err != nil {
		return false, "", err
	}
	return false, action.String(), nil
}

func thresholds(cfg config.Config) model.Thresholds {
	return model.Thresholds{
		MinUptimeHours:   cfg.MinUptimeHours,
		MinMemoryPercent: cfg.MinMemoryPercent,
		ForceUptimeHours: cfg.ForceUptimeHours,
	}
}

func durationHours(hours float64) time.Duration {
	return time.Duration(hours * float64(time.Hour))
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "用法: rebootremind %s [选项]\n", name)
		fs.PrintDefaults()
	}
	return fs
}

func emit(stdout, stderr io.Writer, data any) int {
	if err := emitJSON(stdout, envelope{OK: true, Data: data}); err != nil {
		fmt.Fprintf(stderr, "输出失败: %v\n", err)
		return exitFail
	}
	return exitOK
}

func emitJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func fail(stdout, stderr io.Writer, jsonMode bool, exitCode int, code, message string) int {
	if jsonMode {
		_ = emitJSON(stdout, envelope{OK: false, Error: &errorBody{Code: code, Message: message}})
	}
	fmt.Fprintf(stderr, "错误: %s\n", message)
	return exitCode
}

func printStatus(w io.Writer, status model.Status, decision model.Decision) {
	fmt.Fprintf(w, "系统健康状态：%s\n", healthLabel(status.Health))
	fmt.Fprintf(w, "  开机时长：%s（提醒阈值 %g 小时，强制 %g 小时）\n",
		model.FormatUptime(status.UptimeSeconds), status.Thresholds.MinUptimeHours, status.Thresholds.ForceUptimeHours)
	fmt.Fprintf(w, "  内存占用：%.1f%%（%s / %s）\n",
		status.Memory.Percent, formatGB(status.Memory.UsedBytes), formatGB(status.Memory.TotalBytes))
	fmt.Fprintf(w, "  当前判定：%s（%s）\n", notifyLabel(status.WouldNotify), reasonLabel(decision.Reason))
	if status.SnoozeUntil != nil && status.SnoozeActive {
		fmt.Fprintf(w, "  延迟提醒：至 %s\n", status.SnoozeUntil.Local().Format("2006-01-02 15:04"))
	} else {
		fmt.Fprintln(w, "  延迟提醒：无")
	}
	if len(status.TopProcesses) > 0 {
		fmt.Fprintln(w, "内存大户 Top 5：")
		for _, process := range status.TopProcesses {
			fmt.Fprintf(w, "  %-24s %8s  %5.1f%%\n", process.Name, formatMB(process.WorkingSetBytes), process.Percent)
		}
	}
}

func healthLabel(health string) string {
	switch health {
	case "attention":
		return "建议重启（内存与开机时长已达保养阈值）"
	case "watch":
		return "持续观察（开机时间较长但内存仍未达水位）"
	default:
		return "正常"
	}
}

func notifyLabel(shouldNotify bool) string {
	if shouldNotify {
		return "需要提醒重启"
	}
	return "暂不需要重启"
}

func reasonLabel(reason string) string {
	switch reason {
	case model.ReasonSnooze:
		return "延迟提醒冷却期内"
	case model.ReasonUptimeBelowMinimum:
		return "开机时长未达提醒阈值"
	case model.ReasonMemoryBelowMinimum:
		return "内存占用未达提醒水位"
	case model.ReasonUptimeForce:
		return "开机时长已满强制保养阈值"
	case model.ReasonUptimeAndMemory:
		return "开机时长与内存占用同时达阈值"
	case model.ReasonForced:
		return "强制提醒"
	case reasonAnotherInstance:
		return "已有另一个巡检进程在处理"
	default:
		return reason
	}
}

func actionLabel(action string) string {
	switch action {
	case "snooze_short":
		return "4 小时后再提醒"
	case "snooze_long":
		return "明天再提醒"
	case "restart":
		return "立即重启"
	case "restart_cancelled":
		return "取消重启，已改为 4 小时后再提醒"
	case "dismissed":
		return "关闭弹窗，已改为 4 小时后再提醒"
	default:
		return action
	}
}

func formatGB(bytes uint64) string {
	const gb = 1024 * 1024 * 1024
	return fmt.Sprintf("%.1f GB", float64(bytes)/gb)
}

func formatMB(bytes uint64) string {
	const mb = 1024 * 1024
	return fmt.Sprintf("%.0f MB", float64(bytes)/mb)
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `rebootremind - Windows 开机时长与内存健康巡检重启提醒

用法:
  rebootremind <命令> [选项]

命令:
  status                 查询系统健康状态（--json）
  check                  执行巡检（--notify 弹窗，--force 强制，--json）
  task install           注册每小时巡检计划任务（--force 允许临时构建路径）
  task uninstall         卸载巡检计划任务
  task status            查询计划任务状态
  config show            查看当前配置（--json）
  config set <key> <值>  修改配置项
  config path            输出数据目录
  schema                 导出 CLI 契约（JSON）
  version                输出版本信息

提示:
  计划任务命令: "rebootremind.exe" check --notify
  数据目录: %APPDATA%\language_projects\rebootremind\<dev|prod>
`)
}
