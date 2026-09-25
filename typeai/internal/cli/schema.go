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
			Description: "进入连续对话；每行输入一条用户消息，AI 回答流式输出",
			Input:       "终端行输入；/exit 或 /quit 退出",
			Output:      "stdout 为流式回答文本；失败说明写 stderr",
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
