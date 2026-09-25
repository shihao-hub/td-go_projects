// schema.go：CLI 自身契约导出（interface: "cli"）。
// 本项目未提供 MCP server（用户决策，偏离《CLI 工具开发标准》默认双入口，
// 理由记录于 specs/01 设计文档 Non-Goals），本目录即机器侧契约自描述。
// commands 骨架由遍历 newRootCmd() 命令树结构化生成（命令名漂移被结构性
// 消除），args/退出码细节经 commandDocs 手写 map 按命令路径补充；
// 全程纯内存，零 I/O（不读配置、不建数据目录、不联网）。
package cli

import (
	"sort"

	"github.com/spf13/cobra"
)

// argSpec 单个位置参数说明。
type argSpec struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Desc     string `json:"desc"`
}

// commandDoc 手写契约细节（按命令路径索引；骨架生成后按此补充，
// 未登记的命令回退通用条目——新增命令至少保证出现在目录中）。
type commandDoc struct {
	Args   []argSpec `json:"args,omitempty"`
	Output string    `json:"output,omitempty"`
}

var commandDocs = map[string]commandDoc{
	"token set": {
		Args:   []argSpec{{Name: "token", Required: true, Desc: "bigmodel 开放平台 API token，trim 后长度 ≥ 20"}},
		Output: "ConfigView（token 脱敏）",
	},
	"token show":   {Output: "ConfigView（token 脱敏）"},
	"token remove": {Output: "ConfigView"},
	"status":       {Output: "StatusView（level/sampled_at/windows[]，含各窗口百分比与已通知档位）"},
	"config show":  {Output: "ConfigView"},
	"config set": {
		Args: []argSpec{
			{Name: "key", Required: true, Desc: "interval | thresholds | hysteresis | silent"},
			{Name: "value", Required: true, Desc: "interval 正时长 clamp [30s,24h]；thresholds 逗号分隔 1..99；hysteresis 0..30；silent true/false"},
		},
		Output: "ConfigView",
	},
	"schema":  {Output: "本契约目录"},
	"version": {Output: `{"version": "<semver>"}`},
}

// schemaCommand 契约目录中的单条命令。
type schemaCommand struct {
	Name        string    `json:"name"`        // 命令路径（相对 root，空格分层，如 "token set"）
	Description string    `json:"description"` // cobra Short
	Args        []argSpec `json:"args,omitempty"`
	Output      string    `json:"output,omitempty"`
}

// schemaDoc 契约目录根结构。
type schemaDoc struct {
	Name      string          `json:"name"`
	Version   string          `json:"version"`
	Interface string          `json:"interface"` // 固定 "cli"：声明这是 CLI 自身契约而非 MCP 工具目录
	Envelope  schemaEnvelope  `json:"envelope"`
	Commands  []schemaCommand `json:"commands"`
}

type schemaEnvelope struct {
	Success   any `json:"success"`
	Failure   any `json:"failure"`
	ExitCodes any `json:"exit_codes"`
}

// builtinCommands cobra 自动注入、不属于业务契约的命令。
var builtinCommands = map[string]bool{"help": true, "completion": true}

// BuildSchemaDoc 构造契约目录：遍历命令树生成骨架 + 手写细节补充。
func BuildSchemaDoc() (*schemaDoc, error) {
	root := newRootCmd()
	doc := &schemaDoc{
		Name:      "glmquotawatch-gui",
		Version:   Version,
		Interface: "cli",
		Envelope: schemaEnvelope{
			Success:   map[string]any{"ok": true, "data": "<命令对应的结果视图>"},
			Failure:   map[string]any{"ok": false, "error": map[string]string{"code": "业务错误码（bad_args/bad_value/no_token/api_error/upstream_error/internal/…）", "message": "可公开说明"}},
			ExitCodes: map[string]int{"success": 0, "business_error": 1, "bad_args": 2},
		},
		Commands: []schemaCommand{},
	}
	collectCommands(root, "", doc)
	sort.Slice(doc.Commands, func(i, j int) bool { return doc.Commands[i].Name < doc.Commands[j].Name })
	return doc, nil
}

// collectCommands 深度优先收集命令树（过滤 cobra 内建命令与隐藏命令）。
func collectCommands(cmd *cobra.Command, parentPath string, doc *schemaDoc) {
	for _, sub := range cmd.Commands() {
		if sub.Hidden || builtinCommands[sub.Name()] {
			continue
		}
		path := sub.Name()
		if parentPath != "" {
			path = parentPath + " " + sub.Name()
		}
		entry := schemaCommand{
			Name:        path,
			Description: sub.Short,
		}
		if d, ok := commandDocs[path]; ok {
			entry.Args = d.Args
			entry.Output = d.Output
		}
		doc.Commands = append(doc.Commands, entry)
		collectCommands(sub, path, doc)
	}
}
