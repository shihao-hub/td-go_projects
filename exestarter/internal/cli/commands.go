// Package cli 实现子命令分发与各命令逻辑（结构对齐 clictl 的 CLI 骨架模式）。
package cli

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"exestarter/internal/model"
	"exestarter/internal/scan"
)

// Version 版本号
var Version = "dev"

// exitNotFound 透传命令（run）未注册/失效统一退出码，借 shell "not found" 惯例
const exitNotFound = 127

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
	// run 的剩余参数全部透传给子进程，不能剥离 --pretty
	if cmd != "run" {
		rest = stripPretty(rest)
	}

	switch cmd {
	case "scan":
		return cmdScan(rest)
	case "list":
		return cmdList(rest)
	case "add":
		return cmdAdd(rest)
	case "remove":
		return cmdRemove(rest)
	case "prune":
		return cmdPrune(rest)
	case "tag":
		return cmdTag(rest)
	case "update":
		return cmdUpdate(rest)
	case "run":
		return cmdRun(rest)
	case "open":
		return cmdOpen(rest)
	case "shell":
		return cmdShell(rest)
	case "version", "--version", "-v":
		Emit(map[string]string{"version": Version})
		return 0
	case "help", "-h", "--help":
		EmitHelp()
		return 0
	default:
		Fail("unknown_command", "未知命令: "+cmd+"，exestarter help 查看用法")
		return 1
	}
}

// EmitHelp 输出帮助（也是 JSON，受 --pretty 影响）
func EmitHelp() {
	Emit(map[string]any{
		"usage": "exestarter <command> [args...]",
		"global_flags": []map[string]string{
			{"flag": "--pretty", "desc": "缩进 JSON 输出；可位于子命令前后，但 run 的透传段除外"},
		},
		"commands": []map[string]string{
			{"cmd": "scan [--dir D] [--add]", "desc": "递归扫描目录收集 exe（跳过 node_modules 等噪音目录）；--add 全部注册，否则仅列出"},
			{"cmd": "list [--status valid|invalid] [--tag T]", "desc": "已注册条目；--tag 匹配系统/用户标签"},
			{"cmd": "add <path> [--name N] [--systag K] [--usertag T]", "desc": "注册单个 exe；--systag: todo|verify|broken|stable"},
			{"cmd": "remove <name>", "desc": "按 name 删除注册"},
			{"cmd": "prune", "desc": "清理全部失效条目（文件已不存在）"},
			{"cmd": "tag <name> [--systag K] [--usertag T]", "desc": "设置标签；传了才更新，传空串清除，未传保持原值；--systag: todo|verify|broken|stable"},
			{"cmd": "update <name> --path P", "desc": "改路径（exe 搬家）；名称跟随新文件名，标签与添加时间保留"},
			{"cmd": "run <name> [args...]", "desc": "前台透传启动；退出码=子进程码，未注册/失效=127"},
			{"cmd": "open <name>", "desc": "资源管理器中定位该 exe"},
			{"cmd": "shell <name>", "desc": "在该 exe 目录下开新 PowerShell 窗口"},
			{"cmd": "version", "desc": "版本号"},
			{"cmd": "help", "desc": "本帮助"},
		},
		"notes": []string{
			"条目配置位于 UserConfigDir/exestarter/config.json",
			"run 是透传命令：stdout 属于子进程，错误 JSON 走 stderr",
		},
	})
}

// findEntry 按 name 精确查找；重名时 ok=false（调用方自行区分未注册与重名冲突）
func findEntry(s *model.Store, name string) (int, *model.Entry, bool) {
	entries := s.Snapshot()
	idx, dup := -1, false
	for i, e := range entries {
		if e.Name == name {
			if idx >= 0 {
				dup = true
				break
			}
			idx = i
		}
	}
	if idx == -1 || dup {
		return 0, nil, false
	}
	e := entries[idx]
	return idx, &e, true
}

// mustLoad 载入配置并清洗
func mustLoad() *model.Store {
	return model.NewStore(model.LoadConfig().Entries)
}

func mustSave(s *model.Store) {
	c := model.LoadConfig() // 保留 LastScanDir 等其他字段
	c.Entries = s.Snapshot()
	if err := model.SaveConfig(c); err != nil {
		Fail("internal", "保存配置失败: "+err.Error())
	}
}

// cmdScan 扫描目录收集 exe；--add 时全部注册
func cmdScan(args []string) int {
	fs := newFlagSet("scan")
	dir := fs.String("dir", "", "扫描根目录（缺省用上次）")
	add := fs.Bool("add", false, "扫描结果全部注册（按路径去重）")
	flags, _ := splitFlags(args, map[string]bool{"dir": true}, map[string]bool{"add": true})
	if !parseFlags(fs, flags) {
		return 0
	}

	c := model.LoadConfig()
	target := *dir
	if target == "" {
		target = c.LastScanDir
	}
	if target == "" {
		Fail("bad_args", "未指定 --dir 且无上次扫描目录记录")
		return 1
	}
	if fi, err := os.Stat(target); err != nil || !fi.IsDir() {
		Fail("bad_args", "扫描目录不存在: "+target)
		return 1
	}
	abs, _ := filepath.Abs(target)

	exes, err := scan.ScanDirExe(abs)
	if err != nil {
		Fail("internal", "扫描失败: "+err.Error())
		return 1
	}

	if !*add {
		Emit(map[string]any{"dir": abs, "count": len(exes), "exes": exes})
		return 0
	}

	s := model.NewStore(c.Entries)
	added := 0
	for _, p := range exes {
		if s.Add("", p) {
			added++
		}
	}
	c.Entries = s.Snapshot()
	c.LastScanDir = abs
	if err := model.SaveConfig(c); err != nil {
		Fail("internal", "保存配置失败: "+err.Error())
		return 1
	}
	Emit(map[string]any{"dir": abs, "found": len(exes), "added": added, "skipped": len(exes) - added})
	return 0
}

// cmdList 列出条目
func cmdList(args []string) int {
	fs := newFlagSet("list")
	status := fs.String("status", "", "过滤：valid|invalid")
	tag := fs.String("tag", "", "过滤：匹配系统标签或用户标签")
	flags, _ := splitFlags(args, map[string]bool{"status": true, "tag": true}, nil)
	if !parseFlags(fs, flags) {
		return 0
	}
	if *status != "" && *status != "valid" && *status != "invalid" {
		Fail("bad_args", "--status 仅支持 valid|invalid")
		return 1
	}

	s := mustLoad()
	s.RefreshValid()
	out := make([]map[string]any, 0) // 空切片防 null
	for _, e := range s.Snapshot() {
		if *status == "valid" && !e.Valid {
			continue
		}
		if *status == "invalid" && e.Valid {
			continue
		}
		if *tag != "" && e.SysTag != *tag && e.UserTag != *tag {
			continue
		}
		out = append(out, map[string]any{
			"name":     e.Name,
			"path":     e.Path,
			"valid":    e.Valid,
			"sys_tag":  e.SysTag,
			"user_tag": e.UserTag,
			"added_at": e.AddedAt,
		})
	}
	Emit(out)
	return 0
}

// cmdAdd 注册单个 exe
func cmdAdd(args []string) int {
	fs := newFlagSet("add")
	name := fs.String("name", "", "调用名，默认=文件名去 .exe")
	systag := fs.String("systag", "", "系统标签: todo|verify|broken|stable")
	usertag := fs.String("usertag", "", "用户标签（自由文本描述）")
	flags, positional := splitFlags(args, map[string]bool{"name": true, "systag": true, "usertag": true}, nil)
	if !parseFlags(fs, flags) {
		return 0
	}
	if len(positional) != 1 {
		Fail("bad_args", "用法: exestarter add <path> [--name N] [--systag K] [--usertag T]")
		return 1
	}
	path := positional[0]
	if strings.ToLower(filepath.Ext(path)) != ".exe" {
		Fail("bad_args", "仅支持注册 .exe 文件: "+path)
		return 1
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		Fail("bad_args", "路径无效: "+err.Error())
		return 1
	}
	if !model.FileExists(abs) {
		Fail("file_not_found", "文件不存在: "+abs)
		return 1
	}
	if *systag != "" && model.SysTagLabel(*systag) == "" {
		Fail("bad_args", "非法系统标签 "+*systag+"，可选: todo|verify|broken|stable")
		return 1
	}

	s := mustLoad()
	if !s.Add(*name, abs) {
		Fail("conflict", "该路径已注册: "+abs)
		return 1
	}
	// 补写标签与时间（Add 只建骨架）
	entries := s.Snapshot()
	e := &entries[len(entries)-1]
	e.SysTag = model.SanitizeSysTag(*systag)
	e.UserTag = strings.TrimSpace(*usertag)
	e.AddedAt = time.Now().Format("2006-01-02")
	s2 := model.NewStore(entries)
	mustSave(s2)
	Emit(e)
	return 0
}

// cmdRemove 按 name 删除
func cmdRemove(args []string) int {
	if len(args) != 1 {
		Fail("bad_args", "用法: exestarter remove <name>")
		return 1
	}
	s := mustLoad()
	idx, e, ok := findEntry(s, args[0])
	if !ok {
		// 区分未注册与重名冲突
		for _, x := range s.Snapshot() {
			if x.Name == args[0] {
				Fail("conflict", "存在多个同名条目 "+args[0]+"，请手改配置文件区分")
				return 1
			}
		}
		Fail("not_found", "未注册: "+args[0])
		return 1
	}
	s.Remove(idx)
	mustSave(s)
	Emit(map[string]any{"removed": e.Name, "path": e.Path})
	return 0
}

// cmdPrune 清理失效条目
func cmdPrune(args []string) int {
	if len(args) > 0 {
		Fail("bad_args", "prune 不接受参数")
		return 1
	}
	s := mustLoad()
	s.RefreshValid()
	removed := s.RemoveInvalid()
	mustSave(s)
	Emit(map[string]any{"removed": removed, "remaining": len(s.Snapshot())})
	return 0
}

// cmdTag 给已有条目设置标签。fs.Visit 区分"显式传入"与"未传"：
// 传了才更新，传空串 = 清除该标签，未传的保持原值。
func cmdTag(args []string) int {
	fs := newFlagSet("tag")
	fs.String("systag", "", "系统标签: todo|verify|broken|stable（传空串清除）")
	fs.String("usertag", "", "用户标签（自由文本，传空串清除）")
	flags, positional := splitFlags(args, map[string]bool{"systag": true, "usertag": true}, nil)
	if !parseFlags(fs, flags) {
		return 0
	}
	if len(positional) != 1 {
		Fail("bad_args", "用法: exestarter tag <name> [--systag K] [--usertag T]")
		return 1
	}

	s := mustLoad()
	idx, _, ok := findEntry(s, positional[0])
	if !ok {
		for _, x := range s.Snapshot() {
			if x.Name == positional[0] {
				Fail("conflict", "存在多个同名条目 "+positional[0]+"，请手改配置文件区分")
				return 1
			}
		}
		Fail("not_found", "未注册: "+positional[0])
		return 1
	}

	entries := s.Snapshot()
	e := &entries[idx]
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "systag":
			if f.Value.String() != "" && model.SysTagLabel(f.Value.String()) == "" {
				Fail("bad_args", "非法系统标签 "+f.Value.String()+"，可选: todo|verify|broken|stable")
			}
			e.SysTag = model.SanitizeSysTag(f.Value.String())
		case "usertag":
			e.UserTag = strings.TrimSpace(f.Value.String())
		}
	})
	mustSave(model.NewStore(entries))
	Emit(e)
	return 0
}

// cmdUpdate 改路径（exe 搬家后只更新路径）。UpdatePath 内部查重
// （大小写不敏感 Clean 后比较），名称跟随新文件名，标签与 AddedAt 保留。
func cmdUpdate(args []string) int {
	fs := newFlagSet("update")
	path := fs.String("path", "", "新 exe 路径")
	flags, positional := splitFlags(args, map[string]bool{"path": true}, nil)
	if !parseFlags(fs, flags) {
		return 0
	}
	if len(positional) != 1 || *path == "" {
		Fail("bad_args", "用法: exestarter update <name> --path P")
		return 1
	}
	if strings.ToLower(filepath.Ext(*path)) != ".exe" {
		Fail("bad_args", "仅支持 .exe 文件: "+*path)
		return 1
	}
	abs, err := filepath.Abs(*path)
	if err != nil {
		Fail("bad_args", "路径无效: "+err.Error())
		return 1
	}
	if !model.FileExists(abs) {
		Fail("file_not_found", "文件不存在: "+abs)
		return 1
	}

	s := mustLoad()
	idx, _, ok := findEntry(s, positional[0])
	if !ok {
		for _, x := range s.Snapshot() {
			if x.Name == positional[0] {
				Fail("conflict", "存在多个同名条目 "+positional[0]+"，请手改配置文件区分")
				return 1
			}
		}
		Fail("not_found", "未注册: "+positional[0])
		return 1
	}
	if !s.UpdatePath(idx, abs) {
		Fail("conflict", "该路径已注册: "+abs)
		return 1
	}
	mustSave(s)
	e := s.Snapshot()[idx]
	Emit(&e)
	return 0
}

// cmdRun 前台透传启动：stdout/stderr/stdin 全部继承，退出码=子进程码。
// 透传纪律：错误 JSON 走 stderr，未注册/失效=127。
func cmdRun(args []string) int {
	if len(args) == 0 {
		FailStderr("bad_args", "用法: exestarter run <name> [args...]", exitNotFound)
		return exitNotFound
	}
	s := mustLoad()
	_, e, ok := findEntry(s, args[0])
	if !ok {
		FailStderr("not_found", "未注册或重名: "+args[0]+"，exestarter list 查看", exitNotFound)
		return exitNotFound
	}
	if !model.FileExists(e.Path) {
		FailStderr("file_not_found", "文件已失效: "+e.Path, exitNotFound)
		return exitNotFound
	}

	code, err := runForeground(e.Path, args[1:])
	if err != nil {
		FailStderr("spawn_error", "启动失败: "+err.Error(), 1)
		return 1
	}
	return code
}

// cmdOpen 资源管理器定位 exe
func cmdOpen(args []string) int {
	if len(args) != 1 {
		Fail("bad_args", "用法: exestarter open <name>")
		return 1
	}
	s := mustLoad()
	_, e, ok := findEntry(s, args[0])
	if !ok {
		Fail("not_found", "未注册或重名: "+args[0])
		return 1
	}
	if !model.FileExists(e.Path) {
		Fail("file_not_found", "文件已失效: "+e.Path)
		return 1
	}
	if err := openInExplorer(e.Path); err != nil {
		Fail("internal", "打开资源管理器失败: "+err.Error())
		return 1
	}
	Emit(map[string]any{"opened": filepath.Dir(e.Path), "selected": e.Path})
	return 0
}

// cmdShell 在 exe 目录开新 PowerShell 窗口
func cmdShell(args []string) int {
	if len(args) != 1 {
		Fail("bad_args", "用法: exestarter shell <name>")
		return 1
	}
	s := mustLoad()
	_, e, ok := findEntry(s, args[0])
	if !ok {
		Fail("not_found", "未注册或重名: "+args[0])
		return 1
	}
	dir := filepath.Dir(e.Path)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		Fail("file_not_found", "目录不存在: "+dir)
		return 1
	}
	if err := spawnShell(dir); err != nil {
		Fail("internal", "打开 PowerShell 失败: "+err.Error())
		return 1
	}
	Emit(map[string]any{"shell": "powershell", "dir": dir})
	return 0
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
