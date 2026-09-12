// Package cli 实现子命令分发与各命令逻辑（结构对齐 clictl 的 CLI 骨架模式）。
//
// 输出纪律特殊说明：ask/preset 是流式命令（对齐 clictl run 的透传豁免）——
// stdout 属于流式应答文本（或 --json 时的 JSONL 事件流），错误 JSON 走 stderr；
// presets/config 是管理命令，stdout 永远是 JSON 包络。
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"

	"quickask/internal/api"
	"quickask/internal/client"
	"quickask/internal/protocol"
)

// Run 分发子命令，返回进程退出码。
// 无参数进入交互 REPL。
func Run(args []string) int {
	for len(args) > 0 && args[0] == "--pretty" {
		Pretty = true
		args = args[1:]
	}
	if len(args) == 0 {
		return runREPL()
	}

	cmd, rest := args[0], args[1:]
	// ask/preset 的位置参数是提问文本，不做 --pretty 剥离（避免吃掉文本里的 -- 开头词）
	if cmd != "ask" && cmd != "preset" {
		rest = stripPretty(rest)
	}

	switch cmd {
	case "ask":
		return cmdAsk(rest)
	case "preset":
		return cmdPreset(rest)
	case "presets":
		return cmdPresets(rest)
	case "config":
		return cmdConfig(rest)
	case "help", "-h", "--help":
		EmitHelp()
		return 0
	default:
		Fail("unknown_command", "未知命令: "+cmd+"，quickask help 查看用法")
		return 1
	}
}

// EmitHelp 输出帮助（也是 JSON，受 --pretty 影响）
func EmitHelp() {
	Emit(map[string]any{
		"usage": "quickask <command> [args...]（无参数进入交互 REPL）",
		"global_flags": []map[string]string{
			{"flag": "--pretty", "desc": "缩进 JSON 输出（仅管理命令，需放在子命令前）"},
		},
		"commands": []map[string]string{
			{"cmd": "ask <text> --preset <名> [--json]", "desc": "单次提问；text 多参数空格拼接，'-' 从 stdin 读全文；--json 输出 JSONL 事件流"},
			{"cmd": "preset <名> <text>", "desc": "按预设名快捷提问（等价 ask --preset）"},
			{"cmd": "presets [add|remove]", "desc": "预设列表；add <名> --system S [--template T]；remove <名|ID>"},
			{"cmd": "config get|set", "desc": "LLM 配置；set --base-url U --api-key K --model M（部分更新）"},
			{"cmd": "help", "desc": "本帮助"},
		},
		"notes": []string{
			"预设/配置与 GUI 版 aiquick 共享 %APPDATA%\\aiquick\\",
			"ask/preset 为流式命令：stdout 是应答文本，错误 JSON 走 stderr；--pretty 仅管理命令可用",
			"后端 quickaskd 由 CLI 自动拉起并随本次调用退出",
		},
	})
}

// ---------------------------------------------------------------------------
// 客户端生命周期

var (
	cl     *client.Client
	clOnce bool
	curRid atomic.Int64 // 当前流式请求 rid（chunk 事件过滤）
)

// mustClient 惰性拉起后端：ResolveBackend 按优先级找 quickaskd.exe（exe 同目录/cwd/bin）。
// stderr 直接透传终端（quickaskd 日志本身走 stderr）。
func mustClient() *client.Client {
	if !clOnce {
		path, err := client.ResolveBackend("")
		if err != nil {
			FailStderr("backend_not_found", "未找到 quickaskd.exe: "+err.Error(), 1)
		}
		c, err := client.Start(path, os.Stderr)
		if err != nil {
			FailStderr("backend_error", "quickaskd 启动失败: "+err.Error(), 1)
		}
		cl, clOnce = c, true
	}
	return cl
}

func shutdown() {
	if cl != nil {
		cl.Shutdown()
	}
}

// ---------------------------------------------------------------------------
// ask / preset：流式提问

// cmdAsk 单次提问：<text> 位置参数拼接；'-' 从 stdin 读全文
func cmdAsk(args []string) int {
	fs := newFlagSet("ask")
	presetName := fs.String("preset", "", "预设名或 ID（必填，presets 查看）")
	jsonMode := fs.Bool("json", false, "stdout 输出 JSONL 事件流（机器消费）")
	flags, positional := splitFlags(args, map[string]bool{"preset": true}, map[string]bool{"json": true})
	if !parseFlagsQuiet(fs, flags) {
		return 1
	}
	if *presetName == "" {
		FailStderr("bad_args", "缺少 --preset，quickask presets 查看可选预设", 1)
		return 1
	}
	input := joinInput(positional)
	if input == "" {
		FailStderr("bad_args", "提问内容为空", 1)
		return 1
	}
	return doAsk(*presetName, input, *jsonMode)
}

// cmdPreset 按预设名快捷提问
func cmdPreset(args []string) int {
	if len(args) < 2 {
		FailStderr("bad_args", "用法: quickask preset <预设名> <text...>", 1)
		return 1
	}
	return doAsk(args[0], joinInput(args[1:]), false)
}

// doAsk 核心流程：找预设 → 订阅 chunk → CallStream 流式打印。
// 默认 stdout 打 chunk 增量文本；--json 时每 chunk 一行 JSONL，结束附 done 事件。
func doAsk(presetName, input string, jsonMode bool) int {
	c := mustClient()
	defer shutdown()

	preset, err := findPreset(c, presetName)
	if err != nil {
		FailStderr("not_found", "预设不存在: "+presetName, 1)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	curRid.Store(0)
	if jsonMode {
		c.Subscribe(api.EventChunk, func(ev protocol.Event) {
			if ev.RID == 0 || ev.RID != curRid.Load() {
				return
			}
			var d api.ChunkData
			if json.Unmarshal(ev.Data, &d) == nil && d.Text != "" {
				emitJSONL(map[string]any{"event": "chunk", "text": d.Text})
			}
		})
	} else {
		c.Subscribe(api.EventChunk, func(ev protocol.Event) {
			if ev.RID == 0 || ev.RID != curRid.Load() {
				return
			}
			var d api.ChunkData
			if json.Unmarshal(ev.Data, &d) == nil && d.Text != "" {
				fmt.Print(d.Text)
			}
		})
	}

	var res api.AskResult
	_, err = c.CallStream(ctx, "ask.stream", api.AskParams{PresetID: preset.ID, Input: input}, &res, func(id int64) {
		curRid.Store(id)
	})
	if err != nil {
		if !jsonMode {
			fmt.Println() // 已打印的部分文本换行，错误 JSON 独立成行
		}
		failFromProto(err)
		return 1
	}
	if jsonMode {
		emitJSONL(map[string]any{"event": "done", "text": res.Text})
	} else {
		fmt.Println()
	}
	return 0
}

// joinInput 拼接位置参数为提问文本；'-' 表示从 stdin 读全文（管道友好）
func joinInput(positional []string) string {
	if len(positional) == 1 && positional[0] == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	return strings.TrimSpace(strings.Join(positional, " "))
}

// findPreset 按 name 或 ID 精确查找
func findPreset(c *client.Client, nameOrID string) (api.Preset, error) {
	var ps []api.Preset
	if _, err := c.Call(context.Background(), "presets.list", nil, &ps); err != nil {
		return api.Preset{}, err
	}
	for _, p := range ps {
		if p.Name == nameOrID || p.ID == nameOrID {
			return p, nil
		}
	}
	return api.Preset{}, fmt.Errorf("preset not found: %s", nameOrID)
}

// failFromProto 协议错误码 → CLI 错误码映射（stderr）
func failFromProto(err error) {
	var pe *protocol.Error
	if errors.As(err, &pe) {
		code := "internal"
		switch pe.Code {
		case protocol.CodeUpstream:
			code = "upstream"
		case protocol.CodeNotFound:
			code = "not_found"
		case protocol.CodeBadRequest:
			code = "bad_args"
		case protocol.CodeCancelled:
			code = "cancelled"
		}
		FailStderr(code, pe.Message, 1)
		return
	}
	FailStderr("internal", err.Error(), 1)
}

// emitJSONL 单行 JSON 事件（流式模式）
func emitJSONL(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Println(string(b))
}

// ---------------------------------------------------------------------------
// presets：列表与管理

func cmdPresets(args []string) int {
	if len(args) == 0 {
		return presetsList()
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return presetsAdd(rest)
	case "remove":
		return presetsRemove(rest)
	default:
		Fail("unknown_command", "未知子命令: "+sub+"（可用: add|remove）")
		return 1
	}
}

func presetsList() int {
	c := mustClient()
	defer shutdown()
	var ps []api.Preset
	if _, err := c.Call(context.Background(), "presets.list", nil, &ps); err != nil {
		Fail("internal", "读取预设失败: "+err.Error())
		return 1
	}
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		out = append(out, map[string]any{"id": p.ID, "name": p.Name, "system": p.System, "user_template": p.UserTemplate})
	}
	Emit(out)
	return 0
}

func presetsAdd(args []string) int {
	fs := newFlagSet("presets add")
	system := fs.String("system", "", "系统提示词（必填）")
	template := fs.String("template", "", "用户消息模板，可含 {{input}} 占位符（可选）")
	flags, positional := splitFlags(args, map[string]bool{"system": true, "template": true}, nil)
	if !parseFlags(fs, flags) {
		return 0
	}
	if len(positional) != 1 || strings.TrimSpace(*system) == "" {
		Fail("bad_args", "用法: quickask presets add <名> --system S [--template T]")
		return 1
	}
	c := mustClient()
	defer shutdown()
	var saved api.Preset
	if _, err := c.Call(context.Background(), "presets.save", api.Preset{
		Name:         positional[0],
		System:       *system,
		UserTemplate: *template,
	}, &saved); err != nil {
		Fail("internal", "保存预设失败: "+err.Error())
		return 1
	}
	Emit(saved)
	return 0
}

func presetsRemove(args []string) int {
	if len(args) != 1 {
		Fail("bad_args", "用法: quickask presets remove <名|ID>")
		return 1
	}
	c := mustClient()
	defer shutdown()
	preset, err := findPreset(c, args[0])
	if err != nil {
		Fail("not_found", "预设不存在: "+args[0])
		return 1
	}
	if _, err := c.Call(context.Background(), "presets.delete", api.PresetIDParams{ID: preset.ID}, nil); err != nil {
		Fail("internal", "删除预设失败: "+err.Error())
		return 1
	}
	Emit(map[string]any{"removed": preset.Name, "id": preset.ID})
	return 0
}

// ---------------------------------------------------------------------------
// config：LLM 配置

func cmdConfig(args []string) int {
	if len(args) == 0 {
		Fail("bad_args", "用法: quickask config get|set")
		return 1
	}
	c := mustClient()
	defer shutdown()

	switch args[0] {
	case "get":
		var cfg api.Config
		if _, err := c.Call(context.Background(), "config.get", nil, &cfg); err != nil {
			Fail("internal", "读取配置失败: "+err.Error())
			return 1
		}
		Emit(cfg)
		return 0
	case "set":
		return configSet(c, args[1:])
	default:
		Fail("unknown_command", "未知子命令: "+args[0]+"（可用: get|set）")
		return 1
	}
}

func configSet(c *client.Client, args []string) int {
	// 部分更新：先读现值，只覆盖显式传入的字段
	var cur api.Config
	if _, err := c.Call(context.Background(), "config.get", nil, &cur); err != nil {
		Fail("internal", "读取配置失败: "+err.Error())
		return 1
	}
	fs := newFlagSet("config set")
	baseURL := fs.String("base-url", cur.BaseURL, "LLM API 地址")
	apiKey := fs.String("api-key", cur.APIKey, "API Key")
	model := fs.String("model", cur.Model, "模型名")
	flags, _ := splitFlags(args, map[string]bool{"base-url": true, "api-key": true, "model": true}, nil)
	if !parseFlags(fs, flags) {
		return 0
	}
	if _, err := c.Call(context.Background(), "config.set", api.Config{
		BaseURL: *baseURL,
		APIKey:  *apiKey,
		Model:   *model,
	}, nil); err != nil {
		Fail("internal", "保存配置失败: "+err.Error())
		return 1
	}
	Emit(map[string]any{"base_url": *baseURL, "model": *model, "api_key_set": *apiKey != ""})
	return 0
}

// ---------------------------------------------------------------------------
// REPL：无参数进入

func runREPL() int {
	c := mustClient()
	defer shutdown()

	var ps []api.Preset
	if _, err := c.Call(context.Background(), "presets.list", nil, &ps); err != nil {
		fmt.Fprintln(os.Stderr, "读取预设失败:", err)
		return 1
	}
	fmt.Println("quickask 交互模式（Ctrl+C 或输入 :q 退出）")
	fmt.Println("可用预设：")
	for i, p := range ps {
		fmt.Printf("  [%d] %s（%s）\n", i+1, p.Name, p.ID)
	}
	fmt.Print("选择预设（序号或名称，默认 1）：")
	choice := strings.TrimSpace(readLine())
	var preset api.Preset
	if choice == "" {
		preset = ps[0]
	} else {
		idx := -1
		if n, err := fmt.Sscanf(choice, "%d", &idx); err == nil && n == 1 && idx >= 1 && idx <= len(ps) {
			preset = ps[idx-1]
		} else {
			for _, p := range ps {
				if p.Name == choice || p.ID == choice {
					preset = p
					break
				}
			}
		}
	}
	if preset.ID == "" {
		fmt.Fprintln(os.Stderr, "无效选择")
		return 1
	}
	fmt.Printf("已选预设：%s。输入内容回车发送：\n", preset.Name)

	// chunk 订阅只做一次（循环内重复订阅会叠加回调）
	c.Subscribe(api.EventChunk, func(ev protocol.Event) {
		if ev.RID == 0 || ev.RID != curRid.Load() {
			return
		}
		var d api.ChunkData
		if json.Unmarshal(ev.Data, &d) == nil && d.Text != "" {
			fmt.Print(d.Text)
		}
	})

	// Ctrl+C 直接退出（REPL 中不打断单次流式）
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for {
		fmt.Print("> ")
		line := readLine()
		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}
		if input == ":q" || input == ":quit" || input == ":exit" {
			return 0
		}
		curRid.Store(0)
		var res api.AskResult
		_, err := c.CallStream(ctx, "ask.stream", api.AskParams{PresetID: preset.ID, Input: input}, &res, func(id int64) {
			curRid.Store(id)
		})
		if err != nil {
			fmt.Println("\n[错误]", err)
			continue
		}
		fmt.Println()
	}
}

func readLine() string {
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

// ---------------------------------------------------------------------------
// flag 骨架（对齐 clictl 模式）

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags 管理命令版：帮助/错误走 stdout JSON 包络
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

// parseFlagsQuiet 流式命令版：错误走 stderr（stdout 属于应答文本）
func parseFlagsQuiet(fs *flag.FlagSet, flags []string) bool {
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "用法见: quickask help（帮助走管理命令通道）")
			return false
		}
		FailStderr("bad_args", fs.Name()+": "+err.Error(), 1)
		return false
	}
	return true
}

// splitFlags 把 args 拆成 (flag 段, 位置参数段)，支持混合顺序。
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
