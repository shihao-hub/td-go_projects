package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	lock "instancelock"
)

// 退出码语义（协议冻结项）：
//
//	0 成功（含 help/version/list）；3 key 已被其他实例持有（业务结果）；1 参数/系统错误
const (
	exitOK    = 0
	exitError = 1
	exitHeld  = 3
)

const usageText = `instancelock - 通用单实例检测工具（基于文件锁，key 相互隔离）

stdout 恒为 JSON 包络: {"ok":true,"data":...} / {"ok":false,"error":{"code","message"}}
--pretty 开启缩进输出（人读）

用法:
  instancelock try  --key KEY [--wait 5s]            快照检查: free=true 空闲（查完即走，不持锁）
  instancelock hold --key KEY [--wait 5s] [--ppid N] 持锁模式: 输出 held=true 的 JSON 后常驻阻塞，
                                                     由本进程替宿主持有锁
  instancelock list                                   列出所有 key 的当前持有状态
  instancelock version                                版本与协议信息

退出码:
  0  成功（含 help/version/list）
  3  key 已被其他实例持有（data.free=false，holder 携带持有者信息）
  1  参数或系统错误（error.code: bad_args | internal）

宿主项目接入（hold 模式）:
  1. 启动时 spawn:  instancelock hold --key my-app [--ppid <宿主PID>]
     （不给 --ppid 时保持子进程 stdin 管道不断开，宿主死后管道 EOF 自动释放）
  2. 读到子进程 stdout 的 JSON 行 data.held=true -> 继续启动；读到退出码 3 -> 宿主自行退出
  3. 宿主无论正常退出、崩溃还是被 kill，锁均由操作系统兜底释放，不会死锁

锁文件: %APPDATA%\language_projects\instancelock\<key前缀>-<hash12>.lock
key 建议带命名空间，如 com.company.appname`

// run 命令入口，返回进程退出码
func run(args []string) int {
	args = stripPretty(args)
	if len(args) == 0 {
		emitHelp()
		return exitOK
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "try":
		return cmdTry(rest)
	case "hold":
		return cmdHold(rest)
	case "list":
		return cmdList(rest)
	case "version", "-v", "--version":
		Emit(map[string]any{"name": "instancelock", "version": version, "protocol": protocolVersion})
		return exitOK
	case "help", "-h", "--help":
		emitHelp()
		return exitOK
	default:
		return Fail("bad_args", fmt.Sprintf("未知命令 %q（见 instancelock help）", cmd))
	}
}

// stripPretty 过滤全局 --pretty 并置位 Pretty（本项目无透传命令，统一过滤即可）
func stripPretty(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--pretty" || a == "-pretty" {
			Pretty = true
			continue
		}
		out = append(out, a)
	}
	return out
}

// newFlagSet 屏蔽 flag 包默认 usage 输出（防污染 stdout）
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags 解析 flag。返回 -1 表示解析成功继续执行；>=0 为应立即退出的退出码
// （-h/--help 按 clictl 模式输出 JSON 帮助并以 0 退出）
func parseFlags(fs *flag.FlagSet, args []string) int {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			emitHelp()
			return exitOK
		}
		return Fail("bad_args", fs.Name()+": "+err.Error())
	}
	return -1
}

func emitHelp() {
	Emit(map[string]any{
		"usage": usageText,
		"commands": []map[string]string{
			{"name": "try", "summary": "快照检查：free=true 空闲 / 退出码 3 已有实例（不持锁）"},
			{"name": "hold", "summary": "持锁模式：输出 held=true 的 JSON 后常驻阻塞，替宿主持有锁"},
			{"name": "list", "summary": "列出所有 key 的当前持有状态"},
			{"name": "version", "summary": "输出版本与协议信息"},
		},
	})
}

// emitHeld 输出"已被持有"的业务结果（A 方案：查询成功，free=false），返回退出码 3
func emitHeld(key string) int {
	holder := map[string]any{"pid": 0, "host": "", "started": ""}
	if info, err := lock.ReadInfo(lock.PathOf(key)); err == nil {
		holder = map[string]any{
			"pid":     info.PID,
			"host":    info.Host,
			"started": info.Started.Format(time.RFC3339),
		}
	}
	Emit(map[string]any{"key": key, "free": false, "holder": holder})
	return exitHeld
}

func cmdTry(args []string) int {
	fs := newFlagSet("try")
	key := fs.String("key", "", "唯一标识本项目的 key（必填）")
	wait := fs.Duration("wait", 0, "等待已有实例释放的时长，如 5s（默认 0=不等待）")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	if *key == "" {
		return Fail("bad_args", "try: 缺少 --key")
	}
	lk, err := lock.TryWait(*key, *wait)
	if err != nil {
		if errors.Is(err, lock.ErrHeld) {
			return emitHeld(*key)
		}
		return Fail("internal", "try: "+err.Error())
	}
	_ = lk.Close()
	Emit(map[string]any{"key": *key, "free": true})
	return exitOK
}

func cmdHold(args []string) int {
	fs := newFlagSet("hold")
	key := fs.String("key", "", "唯一标识本项目的 key（必填）")
	wait := fs.Duration("wait", 0, "等待已有实例释放的时长，如 5s（默认 0=不等待）")
	ppid := fs.Int("ppid", 0, "宿主进程 PID：给定后改为轮询宿主存活（stdin EOF 不再触发退出）")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	if *key == "" {
		return Fail("bad_args", "hold: 缺少 --key")
	}
	lk, err := lock.TryWait(*key, *wait)
	if err != nil {
		if errors.Is(err, lock.ErrHeld) {
			return emitHeld(*key)
		}
		return Fail("internal", "hold: "+err.Error())
	}
	defer lk.Close()
	if err := lk.WriteInfo(*key); err != nil {
		// 警告走 stderr，不影响 stdout JSON 纯净
		fmt.Fprintln(os.Stderr, "警告: 写入持有者信息失败:", err)
	}
	Emit(map[string]any{"key": *key, "held": true, "pid": os.Getpid()})
	return holdBlock(*ppid)
}

// holdBlock 持锁期间的常驻阻塞逻辑（包级变量，测试中可替换以短路）：
// ppid>0 轮询宿主存活；否则阻塞于 stdin EOF。收到中断信号（Ctrl+C / SIGTERM）释放退出
var holdBlock = func(ppid int) int {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	if ppid > 0 {
		for lock.ProcessAlive(ppid) {
			select {
			case <-sig:
				return exitOK
			case <-time.After(time.Second):
			}
		}
		return exitOK
	}
	stdinDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(stdinDone)
	}()
	select {
	case <-sig:
	case <-stdinDone:
	}
	return exitOK
}

func cmdList(args []string) int {
	fs := newFlagSet("list")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	entries, err := lock.List()
	if err != nil {
		return Fail("internal", "list: "+err.Error())
	}
	// 空列表初始化为 []，避免输出 null
	items := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		key := e.Info.Key
		if key == "" {
			key = "-"
		}
		items = append(items, map[string]any{
			"key":     key,
			"held":    e.Held,
			"pid":     e.Info.PID,
			"host":    e.Info.Host,
			"started": e.Info.Started.Format(time.RFC3339),
			"file":    filepath.Base(e.Path),
		})
	}
	Emit(map[string]any{"entries": items})
	return exitOK
}
