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
	out.WriteString("对话按键: Enter 发送, Ctrl+J 换行, Alt+F / Ctrl+B / Ctrl+Shift+F fork 分支, Ctrl+Left/Right (或 Ctrl+Up/Dn) 切换分支, Ctrl+V 文本粘贴, Alt+V 图片粘贴, Ctrl+T thinking, Ctrl+E 折叠, PgUp/PgDn 滚动\n")
	out.WriteString("对话命令: /fork, /branch <id>, /branch rm [id], /branch rename [old-id] <new-name>, /resume <session-id>, /image <路径>, /detach, /exit, /quit\n")
	out.WriteString("配置环境变量: TYPEAI_BASE_URL, TYPEAI_API_KEY, TYPEAI_MODEL\n")
	fmt.Print(out.String())
}
