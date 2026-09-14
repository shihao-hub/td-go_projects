// Package cli 实现子命令分发与各命令逻辑（结构对齐 clictl 的 CLI 骨架模式）。
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"filesync/config"
	"filesync/engine"
	"filesync/logging"
	"filesync/models"
)

// Run 分发子命令，返回进程退出码
func Run(args []string) int {
	for len(args) > 0 && args[0] == "--pretty" {
		Pretty = true
		args = args[1:]
	}
	if len(args) == 0 {
		EmitHelp()
		return 0
	}

	cmd, rest := args[0], args[1:]
	rest = stripPretty(rest) // 全部是管理命令，无透传段

	switch cmd {
	case "run":
		return cmdRun(rest)
	case "list":
		return cmdList(rest)
	case "add":
		return cmdAdd(rest)
	case "remove":
		return cmdRemove(rest)
	case "help", "-h", "--help":
		EmitHelp()
		return 0
	default:
		Fail("unknown_command", "未知命令: "+cmd+"，filesync help 查看用法")
		return 1
	}
}

// EmitHelp 输出帮助（也是 JSON，受 --pretty 影响）
func EmitHelp() {
	Emit(map[string]any{
		"usage": "filesync <command> [args...]",
		"global_flags": []map[string]string{
			{"flag": "--pretty", "desc": "缩进 JSON 输出，可位于子命令前后"},
		},
		"commands": []map[string]string{
			{"cmd": "run <name> [--dry-run] [--force] [--yes]", "desc": "执行同步任务；--dry-run 只算差异，--force 全量内容校验，--yes 确认删除（默认拒删并在结果中报告）"},
			{"cmd": "list", "desc": "全部任务"},
			{"cmd": "add <name> <source> <target> [--rule R]...", "desc": "注册任务；--rule 忽略规则可多次（gitignore 风格，后规则胜出）"},
			{"cmd": "remove <name>", "desc": "删除任务"},
			{"cmd": "help", "desc": "本帮助"},
		},
		"notes": []string{
			"任务配置与 GUI 版共享 ~/.file-sync/config.json",
			"进度渲染走 stderr，stdout 只输出最终 JSON 结果",
			"watch（持续监听）模式暂未实现，后续版本提供",
		},
	})
}

// mustLoadConfig 加载任务配置（无文件返回空配置，不报错）
func mustLoadConfig() *config.Config {
	c, err := config.Load("")
	if err != nil {
		Fail("config_error", "读取配置失败: "+err.Error())
	}
	return c
}

// findTaskByName 按 name 查任务（CLI 面向 name，config 层面向 ID）
func findTaskByName(c *config.Config, name string) *models.SyncTask {
	for _, t := range c.ListTasks() {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// cmdList 列出全部任务
func cmdList(args []string) int {
	if !noExtraArgs("list", args) {
		return 1
	}
	tasks := mustLoadConfig().ListTasks()
	out := make([]map[string]any, 0, len(tasks)) // 初始化空切片，避免 null
	for _, t := range tasks {
		out = append(out, map[string]any{
			"name":      t.Name,
			"id":        t.ID,
			"source":    t.SourcePath,
			"target":    t.TargetPath,
			"rules":     len(t.IgnoreRules),
			"last_sync": t.LastSync.Format(time.RFC3339),
			"enabled":   t.Enabled,
		})
	}
	Emit(out)
	return 0
}

// cmdAdd 注册任务：校验源目录存在
func cmdAdd(args []string) int {
	fs := newFlagSet("add")
	rules := &stringList{}
	fs.Var(rules, "rule", "忽略规则（gitignore 风格），可多次")
	flags, positional := splitFlags(args, map[string]bool{"rule": true}, nil)
	if !parseFlags(fs, flags) {
		return 0
	}
	if len(positional) != 3 {
		Fail("bad_args", "用法: filesync add <name> <source> <target> [--rule R]...")
		return 1
	}
	name, source, target := positional[0], positional[1], positional[2]

	c := mustLoadConfig()
	if findTaskByName(c, name) != nil {
		Fail("conflict", "同名任务已存在: "+name)
		return 1
	}
	absSource, err := filepath.Abs(source)
	if err != nil {
		Fail("bad_args", "源路径无效: "+err.Error())
		return 1
	}
	if fi, err := os.Stat(absSource); err != nil || !fi.IsDir() {
		Fail("bad_args", "源目录不存在: "+absSource)
		return 1
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		Fail("bad_args", "目标路径无效: "+err.Error())
		return 1
	}

	var ignored []string
	for _, r := range rules.items {
		r = strings.TrimSpace(r)
		if r != "" {
			ignored = append(ignored, r)
		}
	}
	task := models.NewSyncTask(name, absSource, absTarget, ignored)
	if err := c.AddTask(task); err != nil {
		Fail("internal", "保存任务失败: "+err.Error())
		return 1
	}
	Emit(task)
	return 0
}

// cmdRemove 按 name 删除任务
func cmdRemove(args []string) int {
	if len(args) != 1 {
		Fail("bad_args", "用法: filesync remove <name>")
		return 1
	}
	c := mustLoadConfig()
	t := findTaskByName(c, args[0])
	if t == nil {
		Fail("not_found", "任务不存在: "+args[0])
		return 1
	}
	if err := c.DeleteTask(t.ID); err != nil {
		Fail("internal", "删除任务失败: "+err.Error())
		return 1
	}
	Emit(map[string]any{"removed": t.Name, "id": t.ID})
	return 0
}

// cmdRun 执行同步：进度走 stderr，最终 JSON 走 stdout。
// 删除确认：--yes 全部确认；默认拒绝（脚本安全），待删清单见结果 diff.deleted。
func cmdRun(args []string) int {
	fs := newFlagSet("run")
	dryRun := fs.Bool("dry-run", false, "只计算差异不落盘")
	force := fs.Bool("force", false, "全量内容校验（绕过哈希缓存快速判定）")
	yes := fs.Bool("yes", false, "确认执行删除（默认拒绝删除）")
	flags, positional := splitFlags(args, nil, map[string]bool{"dry-run": true, "force": true, "yes": true})
	if !parseFlags(fs, flags) {
		return 0
	}
	if len(positional) != 1 {
		Fail("bad_args", "用法: filesync run <name> [--dry-run] [--force] [--yes]")
		return 1
	}

	c := mustLoadConfig()
	task := findTaskByName(c, positional[0])
	if task == nil {
		Fail("not_found", "任务不存在: "+positional[0])
		return 1
	}

	// 日志与哈希缓存：与 GUI 版共享 ~/.file-sync/
	if err := logging.Init(syncLogPath()); err != nil {
		Fail("internal", "初始化日志失败: "+err.Error())
		return 1
	}
	cache := engine.LoadHashCache(hashCachePath())

	// Ctrl+C 触发取消（engine 内部各阶段检查 ctx）
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	tracker := engine.NewTracker(task.ID)
	tracker.SetNotify(renderProgress)

	res, err := engine.Run(ctx, task, cache, tracker, engine.Options{
		Force:  *force,
		DryRun: *dryRun,
		OnPendingDeletes: func(deletes []string) bool {
			if *yes {
				return true
			}
			fmt.Fprintf(os.Stderr, "\n待删除 %d 项已拒绝（--yes 可执行删除），清单见结果 diff.deleted\n", len(deletes))
			return false
		},
	})
	if err != nil {
		Fail("sync_error", "同步失败: "+err.Error())
		return 1
	}
	fmt.Fprintln(os.Stderr) // 进度行结束后换行，与 JSON 输出分隔

	// 空列表防御：nil 序列化为 null，统一补成 []（参考文档输出纪律）
	if res.Errors == nil {
		res.Errors = []string{}
	}
	if res.Diff.Added == nil {
		res.Diff.Added = []string{}
	}
	if res.Diff.Modified == nil {
		res.Diff.Modified = []string{}
	}
	if res.Diff.Deleted == nil {
		res.Diff.Deleted = []string{}
	}

	// 成功后更新 last_sync（dry-run 不更新）
	if !*dryRun {
		task.LastSync = time.Now()
		if err := c.UpdateTask(task); err != nil {
			res.Errors = append(res.Errors, "更新 last_sync 失败: "+err.Error())
		}
	}

	Emit(map[string]any{
		"task":             task.Name,
		"dry_run":          *dryRun,
		"copied":           res.Copied,
		"skipped":          res.Skipped,
		"deleted":          res.Deleted,
		"deleted_declined": res.DeletedDeclined,
		"errors":           res.Errors,
		"diff":             res.Diff,
	})
	return 0
}

// renderProgress 进度渲染到 stderr（stdout 只留最终 JSON）。
// 单行 \r 覆盖，按状态展示不同维度。
func renderProgress(p models.SyncProgress) {
	switch p.Status {
	case models.StatusScanning:
		fmt.Fprintf(os.Stderr, "\r扫描中: %d 个文件 | %s", p.ScannedFiles, p.CurrentPath)
	case models.StatusCopying:
		eta := ""
		if p.ETASeconds > 0 {
			eta = fmt.Sprintf(" | ETA %ds", p.ETASeconds)
		}
		fmt.Fprintf(os.Stderr, "\r复制中: %d/%d (%.1f%%)%s | %s",
			p.DoneFiles, p.TotalFiles, p.Percentage, eta, p.CurrentPath)
	case models.StatusDeleting:
		fmt.Fprintf(os.Stderr, "\r删除中: %d/%d", p.DoneFiles, p.TotalFiles)
	case models.StatusAwaitingDelete:
		fmt.Fprintf(os.Stderr, "\r复制完成，%d 项待删除确认", len(p.PendingDeletes))
	}
}

// hashCachePath 全局共享哈希缓存（数据目录下，跨任务共享）
func hashCachePath() string {
	dir, err := config.DataDir()
	if err != nil {
		return "hash-cache.gob"
	}
	return filepath.Join(dir, "hash-cache.gob")
}

// syncLogPath CLI 版独立日志文件（数据目录下）
func syncLogPath() string {
	dir, err := config.DataDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "filesync.log")
}

func noExtraArgs(name string, args []string) bool {
	if len(args) > 0 {
		Fail("bad_args", name+" 不接受参数: "+strings.Join(args, " "))
		return false
	}
	return true
}

// stringList 可重复的字符串 flag（标准库 flag 无 StringArray，用 flag.Value 自实现）
type stringList struct {
	items []string
}

func (s *stringList) String() string {
	return strings.Join(s.items, ",")
}

func (s *stringList) Set(v string) error {
	s.items = append(s.items, v)
	return nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parseFlags(fs *flag.FlagSet, flags []string) bool {
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			EmitHelp()
			return false
		}
		Fail("bad_args", fs.Name()+": "+err.Error())
		return false
	}
	return true
}

// splitFlags 把 args 拆成 (flag 段, 位置参数段)，支持混合顺序。
// known：带值 flag；knownBool：布尔 flag。nil 表示无该类 flag。
func splitFlags(args []string, known map[string]bool, knownBool map[string]bool) (flags []string, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a != "-" && strings.HasPrefix(a, "-") {
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				flags = append(flags, a)
				continue
			}
			if knownBool != nil && knownBool[name] {
				flags = append(flags, a)
				continue
			}
			if known != nil && known[name] && i+1 < len(args) {
				flags = append(flags, a, args[i+1])
				i++
				continue
			}
			flags = append(flags, a)
			continue
		}
		positional = append(positional, a)
	}
	return flags, positional
}

func stripPretty(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--pretty" {
			Pretty = true
			continue
		}
		out = append(out, a)
	}
	return out
}
