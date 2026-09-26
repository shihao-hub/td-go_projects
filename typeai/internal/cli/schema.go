package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

type commandContract struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Input       string `json:"input"`
	Output      string `json:"output"`
}

func commandContracts() []commandContract {
	return []commandContract{
		{
			Name:        "typeai",
			Description: "TTY 下进入 TUI 连续对话；AI 回答流式渲染 Markdown，thinking 默认折叠",
			Input:       "Enter 发送；Ctrl+J 换行；Ctrl+V 或终端粘贴多行；Alt+V 粘贴剪贴板图片；Ctrl+T thinking；Ctrl+E 折叠长文本；[[image:<路径>]] 图片标记；/image <路径> 和 /detach 管理图片；PgUp/PgDn 滚动；/exit、/quit 或 Ctrl+C 退出",
			Output:      "全屏 TUI，支持文本、本地图片和剪贴板图片多模态请求；AI 文本剥离图片标记，session 保留原始输入；非 TTY 返回错误；失败说明显示在 TUI 错误条",
		},
		{
			Name:        "typeai config get",
			Description: "查看有效配置，不显示 API Key",
			Input:       "[--json]",
			Output:      "人读文本或 {ok,data} JSON 包络",
		},
		{
			Name:        "typeai config set",
			Description: "部分更新配置文件",
			Input:       "[--base-url URL] [--api-key KEY] [--model NAME] [--json]",
			Output:      "人读文本或 {ok,data} JSON 包络",
		},
		{
			Name:        "typeai schema",
			Description: "导出 CLI 契约目录；不读取配置和数据文件",
			Input:       "-",
			Output:      "一个 JSON 目录对象",
		},
		{
			Name:        "typeai version",
			Description: "查看版本",
			Input:       "[--json]",
			Output:      "人读文本或 {ok,data} JSON 包络",
		},
	}
}

func cmdSchema(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "typeai: schema 不接受参数")
		return 2
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{
		"interface": "cli",
		"name":      "typeai",
		"version":   Version,
		"commands":  commandContracts(),
	}); err != nil {
		fmt.Fprintln(os.Stderr, "typeai: schema 输出失败:", err)
		return 1
	}
	return 0
}
