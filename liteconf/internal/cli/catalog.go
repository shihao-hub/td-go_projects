package cli

import "fmt"

// command 是单个子命令的契约条目，为 help 渲染与 schema 导出的同源数据。
type command struct {
	Name    string         `json:"name"`
	Summary string         `json:"summary"`
	Usage   string         `json:"usage"`
	Input   map[string]any `json:"input"`
	Output  map[string]any `json:"output"`
}

// commands 为 liteconf CLI 的命令契约目录，手工维护；
// 帮助文本与 schema 导出均由它派生，避免两处漂移。
var commands = []command{
	{
		Name:    "http",
		Summary: "把全部参数原样透传给 PATH 中的 curlie（HTTPie 语法 + curl 引擎），调试配置中心 HTTP API",
		Usage:   "liteconf http [curlie 参数...]",
		Input: map[string]any{
			"kind":        "passthrough",
			"description": "全部参数原样转交 curlie，liteconf 不解析、不消费、不改写（含 --help、--version 与 -- 之后的参数）",
		},
		Output: map[string]any{
			"kind":   "passthrough",
			"stdout": "curlie 原样输出",
			"stderr": "curlie 原样输出；curlie 缺失时为安装指引",
			"exit_codes": map[string]any{
				"透传": "curlie 的原始退出码",
				"1":  "PATH 中找不到 curlie，或 curlie 启动失败/被信号终止",
			},
		},
	},
	{
		Name:    "mcp",
		Summary: "启动 stdio MCP server（AI 客户端经 mcpServers 子进程接入），暴露 liteconf.discovery / liteconf.config.get / liteconf.config.put 三个工具",
		Usage:   "liteconf mcp [-server URL]",
		Input: map[string]any{
			"server": "liteconf server 地址，默认 http://127.0.0.1:8646",
		},
		Output: map[string]any{
			"kind":   "protocol",
			"stdout": "MCP JSON-RPC 协议消息（stdio 专用，无任何人读输出）",
			"stderr": "启动诊断与运行错误",
			"exit_codes": map[string]any{
				"常驻": "服务直到客户端断开，正常退出返回 0",
				"1":  "server 构建或运行失败",
				"2":  "调用参数错误",
			},
		},
	},
	{
		Name:    "schema",
		Summary: "输出契约目录（JSON）：tools 与 MCP 注册同源，commands 为 CLI 命令契约",
		Usage:   "liteconf schema",
		Input:   map[string]any{"kind": "none"},
		Output: map[string]any{
			"kind":       "json",
			"stdout":     "本契约目录 JSON 对象（含 name/version/tools/commands）",
			"exit_codes": map[string]any{"0": "成功", "1": "读取 MCP 注册视图或写出失败"},
		},
	},
	{
		Name:    "version",
		Summary: "输出版本号（构建期经 -ldflags -X 注入，未注入为 dev）",
		Usage:   "liteconf version",
		Input:   map[string]any{"kind": "none"},
		Output: map[string]any{
			"kind":       "text",
			"stdout":     "版本号一行",
			"exit_codes": map[string]any{"0": "成功"},
		},
	},
	{
		Name:    "help",
		Summary: "显示人读帮助",
		Usage:   "liteconf help",
		Input:   map[string]any{"kind": "none"},
		Output: map[string]any{
			"kind":       "text",
			"stdout":     "用法、子命令列表、示例与退出码约定",
			"exit_codes": map[string]any{"0": "成功"},
		},
	},
}

// renderHelp 向 stdout 渲染人读帮助，内容由 commands 目录派生。
func renderHelp(opt *Options) int {
	lines := []string{
		"liteconf — liteconf 配置中心的调试 CLI",
		"",
		"用法：",
		"  liteconf <子命令> [参数...]",
		"",
		"子命令：",
	}
	for _, c := range commands {
		lines = append(lines, fmt.Sprintf("  %-8s %s", c.Name, c.Summary))
	}
	lines = append(lines,
		"",
		"示例：",
		"  liteconf http GET :8646/api/app1/dev",
		"  liteconf http :8646/api/discovery",
		"  liteconf http PUT :8646/api/app1/dev Content-Type:application/json key=value",
		"  liteconf mcp                     # 启动 stdio MCP server（默认连 http://127.0.0.1:8646）",
		"  liteconf mcp -server http://192.168.1.10:8646",
		"  liteconf schema",
		"",
		"依赖：http 子命令需要 curlie（go install github.com/rs/curlie@latest）；",
		"      mcp 子命令需要 liteconf server 在运行",
		"退出码：0 成功；1 运行失败（如 curlie 缺失）；2 调用参数错误；",
		"        http 子命令透传 curlie 的退出码；mcp 常驻直到客户端断开。",
	)
	for _, l := range lines {
		fmt.Fprintln(opt.Stdout, l)
	}
	return 0
}
