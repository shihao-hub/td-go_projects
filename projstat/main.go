// projstat 是 language_projects 总仓的项目状态标注 CLI：
// 手工标注（PROJECT.toml）+ git 元数据实时采集，合并输出 list / show / next 三种视图。
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"projstat/internal/meta"
	"projstat/internal/report"
	"projstat/internal/scan"
)

const usageText = `projstat - language_projects 项目状态标注 CLI

用法:
  projstat [list] [--lang <l>] [--stage <s>] [--sort name|commit|due] [--json]
  projstat show <name | lang/name>
  projstat set <name> [--stage <s>] [--source-read] [--usable] [--tested]
                        [--summary <s>] [--next <s>] [--due <YYYY-MM-DD>] [--notes <s>] [--lang <l>]
  projstat init
  projstat next [--json]

全局 flag:
  --root <path>   显式指定总仓根目录（自动定位失败时使用）
  --json          以单行 JSON 信封输出（不含颜色码）

布尔 flag 同时支持 --usable 与 --usable=false 两种写法；未写即未标注（三态）。
注意: set 重写 PROJECT.toml 会丢弃手写注释。`

func main() {
	scan.SetupConsole()
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	start := time.Now()
	g, rest := splitGlobal(args)

	cmd := "list" // 无参数默认 list
	if len(rest) > 0 {
		cmd, rest = rest[0], rest[1:]
	}

	switch cmd {
	case "list":
		return cmdList(rest, &g, start)
	case "show":
		return cmdShow(rest, &g, start)
	case "set":
		return cmdSet(rest, &g, start)
	case "init":
		return cmdInit(rest, &g, start)
	case "next":
		return cmdNext(rest, &g, start)
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return 0
	default:
		return usageError(&g, "未知子命令 %q，运行 projstat help 查看用法", cmd)
	}
}

// cmdList 实现 list：过滤 --lang/--stage → 排序 --sort → 表格或 JSON。
func cmdList(args []string, g *global, start time.Time) int {
	fs := newFlagSet(g)
	lang := fs.String("lang", "", "按语言过滤: go|python|rust|typescript")
	stage := fs.String("stage", "", "按阶段过滤: idea|learning|wip|mvp|usable|paused|dropped")
	sortKey := fs.String("sort", "name", "排序: name|commit|due")
	if err := parseCmd(fs, args, g); err != nil {
		return usageError(g, "list: %v", err)
	}
	if *sortKey != "name" && *sortKey != "commit" && *sortKey != "due" {
		return usageError(g, "list: --sort 非法 %q，合法取值: name | commit | due", *sortKey)
	}

	root, err := locateRoot(g)
	if err != nil {
		return runError(g, "%v", err)
	}
	entries, err := scan.Discover(root)
	if err != nil {
		return runError(g, "%v", err)
	}

	entries = filterEntries(entries, *lang, *stage)
	sortEntries(entries, *sortKey)
	scan.CollectGit(root, entries)
	if *sortKey == "commit" {
		// 采集后再排一次，保证最后提交时间参与最终顺序
		sortEntries(entries, *sortKey)
	}

	if g.json {
		data := make([]report.EntryJSON, len(entries))
		for i, e := range entries {
			data[i] = report.EntryToJSON(e)
		}
		fmt.Println(report.Envelope(data, len(data), time.Since(start)))
		return 0
	}
	if len(entries) == 0 {
		fmt.Println("（无匹配项目）")
		return 0
	}
	fmt.Print(report.Table(entries))
	return 0
}

// cmdShow 实现 show <name>：名称解析后输出单项目详情。
func cmdShow(args []string, g *global, start time.Time) int {
	fs := newFlagSet(g)
	if err := parseCmd(fs, args, g); err != nil {
		return usageError(g, "show: %v", err)
	}
	if fs.NArg() != 1 {
		return usageError(g, "show: 需要 1 个参数，用法 projstat show <name | lang/name>")
	}
	target := fs.Arg(0)

	root, err := locateRoot(g)
	if err != nil {
		return runError(g, "%v", err)
	}
	entries, err := scan.Discover(root)
	if err != nil {
		return runError(g, "%v", err)
	}

	e, status, candidates := resolveName(entries, target)
	if status != resolveUnique {
		return reportResolveFail(g, entries, target, status, candidates)
	}
	one := []scan.Entry{e}
	scan.CollectGit(root, one)
	if g.json {
		fmt.Println(report.Envelope(report.EntryToJSON(one[0]), 1, time.Since(start)))
	} else {
		fmt.Print(report.Show(one[0]))
	}
	return 0
}

// cmdSet 实现 set <name> --<field> <value>：仅应用显式设置的 flag，校验失败不写文件。
func cmdSet(args []string, g *global, start time.Time) int {
	fs := newFlagSet(g)
	// flag 值不在此读取，统一经 fs.Visit 按名应用（见下），此处仅完成注册
	fs.String("stage", "", "阶段: idea|learning|wip|mvp|usable|paused|dropped")
	fs.String("lang", "", "语言: go|python|rust|typescript")
	fs.String("summary", "", "一句话总结")
	fs.String("next", "", "下一步动作")
	fs.String("due", "", "下一步截止日期 YYYY-MM-DD")
	fs.String("notes", "", "备注")
	fs.Bool("source-read", false, "是否已读源码（三态）")
	fs.Bool("usable", false, "是否可用（三态）")
	fs.Bool("tested", false, "是否已测试（三态）")
	if err := parseCmd(fs, args, g); err != nil {
		return usageError(g, "set: %v", err)
	}
	if fs.NArg() != 1 {
		return usageError(g, "set: 需要 1 个参数，用法 projstat set <name> --<field> <value>")
	}
	target := fs.Arg(0)

	root, err := locateRoot(g)
	if err != nil {
		return runError(g, "%v", err)
	}
	entries, err := scan.Discover(root)
	if err != nil {
		return runError(g, "%v", err)
	}
	e, status, candidates := resolveName(entries, target)
	if status != resolveUnique {
		return reportResolveFail(g, entries, target, status, candidates)
	}

	m, err := meta.Load(dirOf(root, e))
	if err != nil {
		return runError(g, "%v", err)
	}
	if m == nil {
		m = meta.Skeleton(e.Name, e.Lang)
	}

	// flag.Visit 只遍历显式设置的 flag，天然区分 "--usable=false" 与未写
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "stage":
			m.Stage = meta.Stage(f.Value.String())
		case "lang":
			m.Lang = f.Value.String()
		case "summary":
			m.Summary = f.Value.String()
		case "next":
			m.NextAction = f.Value.String()
		case "due":
			m.NextDue = f.Value.String()
		case "notes":
			m.Notes = f.Value.String()
		case "source-read":
			v := f.Value.String() == "true"
			m.SourceRead = &v
		case "usable":
			v := f.Value.String() == "true"
			m.Usable = &v
		case "tested":
			v := f.Value.String() == "true"
			m.Tested = &v
		}
	})

	if err := m.Validate(); err != nil {
		// REQ-7: 非法取值退出码 2，且不写文件
		return usageError(g, "set %s: %v（未写文件）", target, err)
	}
	if err := meta.Save(dirOf(root, e), m); err != nil {
		return runError(g, "save %s: %v", e.Dir, err)
	}
	if g.json {
		fmt.Println(report.Envelope(report.EntryToJSON(scan.Entry{Dir: e.Dir, Name: e.Name, Lang: e.Lang, Meta: m}), 1, time.Since(start)))
	} else {
		fmt.Printf("updated: %s\n", e.Dir)
	}
	return 0
}

// cmdInit 实现 init：为所有缺 PROJECT.toml 的项目生成骨架，幂等跳过已存在者。
func cmdInit(args []string, g *global, start time.Time) int {
	fs := newFlagSet(g)
	if err := parseCmd(fs, args, g); err != nil {
		return usageError(g, "init: %v", err)
	}
	if fs.NArg() != 0 {
		return usageError(g, "init: 不接受参数")
	}

	root, err := locateRoot(g)
	if err != nil {
		return runError(g, "%v", err)
	}
	entries, err := scan.Discover(root)
	if err != nil {
		return runError(g, "%v", err)
	}
	created, skipped := 0, 0
	for _, e := range entries {
		if e.Meta != nil {
			skipped++
			continue
		}
		if err := meta.Save(dirOf(root, e), meta.Skeleton(e.Name, e.Lang)); err != nil {
			return runError(g, "save %s: %v", e.Dir, err)
		}
		created++
	}
	if g.json {
		fmt.Println(report.Envelope(map[string]int{"created": created, "skipped": skipped}, created, time.Since(start)))
	} else {
		fmt.Printf("created %d, skipped %d\n", created, skipped)
	}
	return 0
}

// cmdNext 实现 next：输出全部填有 next_action 的项目，逾期置顶、due 升序、空值垫底。
func cmdNext(args []string, g *global, start time.Time) int {
	fs := newFlagSet(g)
	if err := parseCmd(fs, args, g); err != nil {
		return usageError(g, "next: %v", err)
	}
	if fs.NArg() != 0 {
		return usageError(g, "next: 不接受参数")
	}

	root, err := locateRoot(g)
	if err != nil {
		return runError(g, "%v", err)
	}
	entries, err := scan.Discover(root)
	if err != nil {
		return runError(g, "%v", err)
	}

	todos := make([]scan.Entry, 0)
	for _, e := range entries {
		if e.Meta != nil && e.Meta.NextAction != "" {
			todos = append(todos, e)
		}
	}
	sortTodos(todos)

	if g.json {
		data := make([]report.EntryJSON, len(todos))
		for i, e := range todos {
			data[i] = report.EntryToJSON(e)
		}
		fmt.Println(report.Envelope(data, len(data), time.Since(start)))
		return 0
	}
	if len(todos) == 0 {
		fmt.Println("（无待办项目）")
		return 0
	}
	fmt.Print(report.NextTable(todos))
	return 0
}
