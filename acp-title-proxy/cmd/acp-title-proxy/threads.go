package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/zedthreads"
)

// runThreads 是 `agy threads` 的入口：管理 Zed 侧会话的 agent 归属与归档状态。
func runThreads(args []string) int {
	if len(args) == 0 {
		printThreadsUsage()
		return 1
	}
	switch strings.ToLower(args[0]) {
	case "list":
		return threadsList(args[1:])
	case "adopt":
		return threadsAdopt(args[1:])
	case "archive":
		return threadsArchive(args[1:])
	case "-h", "--help", "help":
		printThreadsUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n\n", args[0])
		printThreadsUsage()
		return 1
	}
}

func threadsList(args []string) int {
	fs := flag.NewFlagSet("threads list", flag.ContinueOnError)
	dbPath := fs.String("db", "", "Zed db.sqlite 路径（默认自动发现）")
	agent := fs.String("agent", "", "只显示指定 agent_id")
	arch := fs.String("archived", "all", "过滤：all | active | archived")
	limit := fs.Int("limit", 0, "最多显示条数（0 表示不限）")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	path, err := resolveDBPath(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	db, err := zedthreads.OpenRO(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	defer db.Close()

	fmt.Printf("Zed 数据库: %s\n\n", path)

	if *agent == "" {
		stats, err := zedthreads.Agents(db)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			return 1
		}
		fmt.Printf("%-38s %7s %7s %9s %7s  %s\n", "AGENT", "TOTAL", "ACTIVE", "ARCHIVED", "NO-SID", "LAST")
		for _, s := range stats {
			name := s.AgentID
			if name == "" {
				name = "(null)"
			}
			fmt.Printf("%-38s %7d %7d %9d %7d  %s\n",
				name, s.Total, s.Active, s.Archived, s.NoSessionID, fmtZedTime(s.LastUpdated))
		}
		fmt.Println()
	}

	rows, err := zedthreads.List(db, zedthreads.ListOptions{Agent: *agent, Archived: *arch, Limit: *limit})
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	if len(rows) == 0 {
		fmt.Println("没有匹配的会话。")
		return 0
	}

	fmt.Printf("%-4s %-34s %-16s %-9s %s\n", "ARCH", "AGENT", "UPDATED", "SESSION", "TITLE")
	for _, t := range rows {
		archMark := "no"
		if t.Archived {
			archMark = "yes"
		}
		sid := t.SessionID
		if sid == "" {
			sid = "-"
		} else if len(sid) > 8 {
			sid = sid[:8]
		}
		fmt.Printf("%-4s %-34s %-16s %-9s %s\n",
			archMark, truncRunes(t.AgentID, 33), fmtZedTime(t.UpdatedAt), sid, truncRunes(oneLine(t.Title), 60))
	}
	fmt.Printf("\n共 %d 条。\n", len(rows))
	return 0
}

func threadsAdopt(args []string) int {
	fs := flag.NewFlagSet("threads adopt", flag.ContinueOnError)
	dbPath := fs.String("db", "", "Zed db.sqlite 路径（默认自动发现）")
	from := fs.String("from", "antigravity-acp", "原 agent_id（官方条目）")
	to := fs.String("to", "Google Antigravity (auto-title)", "目标 agent_id（settings.json 里的键名）")
	includeArchived := fs.Bool("include-archived", false, "连已归档的会话一起过户")
	limit := fs.Int("limit", 0, "本次最多迁移条数（0 表示全部；建议先 1 条验证）")
	apply := fs.Bool("apply", false, "真正写入（默认 dry-run）")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	path, err := resolveDBPath(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	db, err := zedthreads.OpenRO(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	plan, err := zedthreads.PlanAdopt(db, *from, *to, *includeArchived)
	_ = db.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}

	fmt.Printf("计划：把 %s 的会话过户到 %s\n", *from, *to)
	fmt.Printf("  可迁移 %d 条\n", len(plan.Rows))
	if *limit > 0 && len(plan.Rows) > *limit {
		fmt.Printf("  本次仅取最近 %d 条（--limit），剩余 %d 条留待下次\n", *limit, len(plan.Rows)-*limit)
		plan.Rows = plan.Rows[:*limit]
	}
	if plan.Skipped > 0 {
		fmt.Printf("  跳过已归档 %d 条（加 --include-archived 可一并迁移）\n", plan.Skipped)
	}
	if plan.EmptySID > 0 {
		fmt.Printf("  跳过无 session_id 的空 thread %d 条（可另用 `agy threads archive --agent %s` 归档）\n",
			plan.EmptySID, *from)
	}
	if len(plan.Rows) == 0 {
		fmt.Println("没有需要迁移的会话。")
		return 0
	}

	fmt.Println("\n样例（最多 10 条）：")
	for i, t := range plan.Rows {
		if i >= 10 {
			break
		}
		fmt.Printf("  %s  %s  %s\n", t.SessionID[:min(8, len(t.SessionID))], fmtZedTime(t.UpdatedAt), truncRunes(oneLine(t.Title), 50))
	}

	if !*apply {
		fmt.Println("\n[dry-run] 未写入。确认无误后加 --apply 执行（需先完全退出 Zed）。")
		return 0
	}

	res, err := zedthreads.Apply(path, plan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	fmt.Printf("\n已过户 %d 条。备份: %s\n", res.Updated, res.Backup)
	fmt.Println("下一步：打开 Zed，旧的 Antigravity 会话会出现在 auto-title 名下，点开由共享 daemon 加载。")
	return 0
}

func threadsArchive(args []string) int {
	fs := flag.NewFlagSet("threads archive", flag.ContinueOnError)
	dbPath := fs.String("db", "", "Zed db.sqlite 路径（默认自动发现）")
	agent := fs.String("agent", "", "要归档的 agent_id（如 antigravity-acp）")
	apply := fs.Bool("apply", false, "真正写入（默认 dry-run）")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	path, err := resolveDBPath(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	db, err := zedthreads.OpenRO(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	plan, err := zedthreads.PlanArchive(db, *agent)
	_ = db.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}

	fmt.Printf("计划：归档 %s 名下 %d 条未归档会话\n", *agent, len(plan.Rows))
	if len(plan.Rows) == 0 {
		fmt.Println("没有需要归档的会话。")
		return 0
	}
	if !*apply {
		fmt.Println("[dry-run] 未写入。确认无误后加 --apply 执行（需先完全退出 Zed）。")
		return 0
	}

	res, err := zedthreads.Apply(path, plan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	fmt.Printf("\n已归档 %d 条。备份: %s\n", res.Updated, res.Backup)
	return 0
}

func resolveDBPath(p string) (string, error) {
	if p != "" {
		return p, nil
	}
	return zedthreads.DiscoverDBPath()
}

func printThreadsUsage() {
	fmt.Fprint(os.Stderr, `agy threads - 管理 Zed 会话的 agent 归属与归档

  agy threads list    [--agent ID] [--archived all|active|archived] [--limit N] [--db PATH]
  agy threads adopt   --from ID --to ID [--include-archived] [--limit N] [--apply] [--db PATH]
  agy threads archive --agent ID [--apply] [--db PATH]

说明：
  adopt    把 --from 名下的会话（session_id 不变的）过户到 --to，之后由该 agent 加载。
           目标必须是 settings.json 中 agent_servers 的键名，例如
           "Google Antigravity (auto-title)"。
  写操作默认 dry-run，加 --apply 才写库；写前必须完全退出 Zed，且自动做一致性备份。
`)
}

func fmtZedTime(s string) string {
	if s == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	return t.Local().Format("2006-01-02 15:04")
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
