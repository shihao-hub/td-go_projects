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
		Name:    "schema",
		Summary: "输出 liteconf CLI 自身的命令契约目录（JSON）；无 MCP 入口，interface 固定为 cli",
		Usage:   "liteconf schema",
		Input:   map[string]any{"kind": "none"},
		Output: map[string]any{
			"kind":       "json",
			"stdout":     "本契约目录 JSON 对象（含 name/interface/version/commands）",
			"exit_codes": map[string]any{"0": "成功"},
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
		"",
		"依赖：http 子命令需要 curlie（go install github.com/rs/curlie@latest）",
		"退出码：0 成功；1 运行失败（如 curlie 缺失）；2 调用参数错误；",
		"        http 子命令透传 curlie 的退出码。",
	)
	for _, l := range lines {
		fmt.Fprintln(opt.Stdout, l)
	}
	return 0
}
