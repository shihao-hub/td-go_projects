package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"projstat/internal/report"
	"projstat/internal/scan"
)

// global 保存全局 flag（--root / --json），允许出现在子命令前后任意一侧。
type global struct {
	root string
	json bool
}

// splitGlobal 从参数头部提取全局 flag（--root X / --root=X / --json / --json=false），
// 返回剩余参数供子命令解析；全局 flag 同时也注册进每个子命令的 flag 集，两种写法均生效。
func splitGlobal(args []string) (global, []string) {
	var g global
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--root":
			if i+1 < len(args) {
				g.root = args[i+1]
				i += 2
			} else {
				i++
			}
		case strings.HasPrefix(a, "--root="):
			g.root = strings.TrimPrefix(a, "--root=")
			i++
		case a == "--json":
			g.json = true
			i++
		case strings.HasPrefix(a, "--json="):
			g.json = strings.TrimPrefix(a, "--json=") == "true"
			i++
		default:
			return g, args[i:]
		}
	}
	return g, nil
}

// newFlagSet 创建子命令 flag 集，统一注册 --root/--json 以支持子命令后置写法。
func newFlagSet(g *global) *flag.FlagSet {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // 错误信息由我们统一格式化输出
	fs.String("root", g.root, "总仓根")
	fs.Bool("json", g.json, "JSON 信封输出")
	return fs
}

// parseCmd 解析子命令参数，并把后置的 --root/--json 回写进 global；
// 同时在此刻（已知晓最终 json 与否）刷新颜色开关，管道/文件输出自动无 ANSI。
func parseCmd(fs *flag.FlagSet, args []string, g *global) error {
	ordered, err := reorderArgs(fs, args)
	if err != nil {
		return err
	}
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if v := fs.Lookup("json"); v != nil && v.Value.String() == "true" {
		g.json = true
	}
	if v := fs.Lookup("root"); v != nil && v.Value.String() != "" {
		g.root = v.Value.String()
	}
	report.UseColor = !g.json && isTTY()
	return nil
}

// reorderArgs 把 flag 参数挪到位置参数之前：
// 标准 flag 包遇到首个位置参数即停止解析，而本工具用法是 "set <name> --flag value"，
// 故先重排为 "--flag value <name>" 再交给 fs.Parse。
// 防护：string flag 的下一 token 若形如已注册 flag（典型成因是空字符串参数被 shell 吞掉），
// 视为缺少值并报错，避免把 flag 名误当值写入文件。
func reorderArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			if !strings.Contains(a, "=") {
				name := strings.TrimLeft(a, "-")
				if fl := fs.Lookup(name); fl != nil {
					if bf, ok := fl.Value.(interface{ IsBoolFlag() bool }); !ok || !bf.IsBoolFlag() {
						if i+1 >= len(args) {
							return nil, fmt.Errorf("flag --%s 缺少值", name)
						}
						if isFlagLike(fs, args[i+1]) {
							return nil, fmt.Errorf("flag --%s 缺少值（若要置空请用 --%s= 等号形式）", name, name)
						}
						i++
						flags = append(flags, args[i])
					}
				}
			}
			continue
		}
		positional = append(positional, a)
	}
	return append(flags, positional...), nil
}

// isFlagLike 判断 token 是否形如已注册的 flag 名。
func isFlagLike(fs *flag.FlagSet, tok string) bool {
	if len(tok) > 1 && tok[0] == '-' {
		name := strings.TrimLeft(tok, "-")
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		return fs.Lookup(name) != nil
	}
	return false
}

// isTTY 判断 stdout 是否为终端（字符设备）。
func isTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// locateRoot 解析总仓根：显式 --root 优先，否则从工作目录逐级向上自动定位。
func locateRoot(g *global) (string, error) {
	if g.root != "" {
		return filepath.Abs(g.root)
	}
	return scan.FindRoot(".")
}

// dirOf 返回项目在磁盘上的绝对路径。
func dirOf(root string, e scan.Entry) string {
	return filepath.Join(root, e.Dir)
}

// usageError 输出用法/参数错误并以退出码 2 结束；--json 时额外在 stdout 输出 error 信封。
func usageError(g *global, format string, a ...any) int {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintln(os.Stderr, msg)
	if g != nil && g.json {
		fmt.Println(report.ErrorEnvelope(msg))
	}
	return 2
}

// runError 输出运行错误并以退出码 1 结束；--json 时额外在 stdout 输出 error 信封。
func runError(g *global, format string, a ...any) int {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintln(os.Stderr, msg)
	if g != nil && g.json {
		fmt.Println(report.ErrorEnvelope(msg))
	}
	return 1
}

// resolveStatus 是名称解析结果的三种状态。
type resolveStatus int

const (
	resolveNone      resolveStatus = iota // 无匹配
	resolveUnique                         // 恰好一个
	resolveAmbiguous                      // 多个同名
)

// resolveName 按目录名全局解析项目；支持 "lang/name" 二段式精确消歧。
// 无匹配时 candidates 返回按子串匹配的近似名列表。
func resolveName(entries []scan.Entry, arg string) (scan.Entry, resolveStatus, []string) {
	if i := strings.Index(arg, "/"); i >= 0 {
		lang, name := arg[:i], arg[i+1:]
		for _, e := range entries {
			if e.Lang == lang && e.Name == name {
				return e, resolveUnique, nil
			}
		}
		return scan.Entry{}, resolveNone, nil
	}
	var matches []scan.Entry
	for _, e := range entries {
		if e.Name == arg {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], resolveUnique, nil
	case 0:
		lower := strings.ToLower(arg)
		var similar []string
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.Name), lower) {
				similar = append(similar, e.Name)
			}
		}
		return scan.Entry{}, resolveNone, similar
	default:
		names := make([]string, len(matches))
		for i, e := range matches {
			names[i] = e.Lang + "/" + e.Name
		}
		return scan.Entry{}, resolveAmbiguous, names
	}
}

// reportResolveFail 统一输出 show/set 的名称解析失败（退出码 1）。
func reportResolveFail(g *global, entries []scan.Entry, target string, status resolveStatus, candidates []string) int {
	switch status {
	case resolveAmbiguous:
		return runError(g, "名称歧义 %q，候选: %s；请用 <lang>/<name> 形式", target, strings.Join(candidates, ", "))
	default:
		if len(candidates) > 0 {
			return runError(g, "未找到项目 %q，近似名: %s", target, strings.Join(candidates, ", "))
		}
		return runError(g, "未找到项目 %q", target)
	}
}

// filterEntries 按 --lang/--stage 过滤；未标注项目不匹配任何 stage 过滤条件。
func filterEntries(entries []scan.Entry, lang, stage string) []scan.Entry {
	out := make([]scan.Entry, 0, len(entries))
	for _, e := range entries {
		if lang != "" && e.Lang != lang {
			continue
		}
		if stage != "" && (e.Meta == nil || string(e.Meta.Stage) != stage) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// dueOf 返回项目的 next_due 字符串；未标注返回空串。
func dueOf(e scan.Entry) string {
	if e.Meta == nil {
		return ""
	}
	return e.Meta.NextDue
}

// sortEntries 按 key 排序：name 不分大小写；commit 最后提交降序零值垫底；due 升序空值垫底。
func sortEntries(entries []scan.Entry, key string) {
	switch key {
	case "commit":
		sort.SliceStable(entries, func(i, j int) bool {
			ti, tj := entries[i].Git.LastCommitAt, entries[j].Git.LastCommitAt
			zi, zj := ti.IsZero(), tj.IsZero()
			if zi != zj {
				return zj // 零值垫底
			}
			return ti.After(tj) // 新提交在前
		})
	case "due":
		sort.SliceStable(entries, func(i, j int) bool {
			di, dj := dueOf(entries[i]), dueOf(entries[j])
			if (di == "") != (dj == "") {
				return dj == "" // 空值垫底
			}
			return di < dj
		})
	default: // name
		sort.SliceStable(entries, func(i, j int) bool {
			return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
		})
	}
}

// sortTodos 排序 next 待办：逾期置顶 → due 升序（空值最后）→ name 兜底。
func sortTodos(entries []scan.Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		oi, oj := report.IsOverdue(dueOf(entries[i])), report.IsOverdue(dueOf(entries[j]))
		if oi != oj {
			return oi // 逾期置顶
		}
		di, dj := dueOf(entries[i]), dueOf(entries[j])
		if (di == "") != (dj == "") {
			return dj == "" // 空值垫底
		}
		if di != dj {
			return di < dj
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}
