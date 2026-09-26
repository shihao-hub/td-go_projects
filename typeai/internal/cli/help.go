package cli

import (
	"fmt"
	"strings"
)

func emitHelp() {
	var out strings.Builder
	out.WriteString("typeai - 原始打字机 AI 对话\n\n")
	out.WriteString("用法:\n")
	for _, command := range commandContracts() {
		out.WriteString(fmt.Sprintf("  %-30s %s\n", command.Name, command.Description))
	}
	out.WriteString("\n")
	out.WriteString("对话按键: Enter 发送, Ctrl+J 换行, Ctrl+T thinking, PgUp/PgDn 滚动\n")
	out.WriteString("对话命令: /exit, /quit\n")
	out.WriteString("配置环境变量: TYPEAI_BASE_URL, TYPEAI_API_KEY, TYPEAI_MODEL\n")
	fmt.Print(out.String())
}
