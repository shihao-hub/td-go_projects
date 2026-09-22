package mcp

import (
	"context"

	"agyquota/internal/service"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- 输入类型 ----
// jsonschema tag 即参数描述，SDK 据此推导 JSON Schema（2020-12）。
// 可选字段一律 omitempty（不进 required）；业务校验在 service 层。
// 输出直接复用 service 的 Snapshot 类型，保证 CLI/MCP 两侧同形。

type quotaGetIn struct {
	TokenFile string `json:"tokenFile,omitempty" jsonschema:"可选：凭据文件路径；缺省用 ~/.gemini/antigravity-acp/acp_token.json"`
}

// registerTools 注册全部 MCP 工具（与 schema 子命令导出同源）。
// 工具名规则：agyquota.<资源>.<动词> 三段式，与 CLI 命令路径对应
// （quota → agyquota.quota.get）。业务错误返回普通 error
//（*service.Error 文本即 "code: message"），SDK 自动转 IsError 结果。
func registerTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "agyquota.quota.get",
		Description: "查询 Google Antigravity 模型配额：Gemini 与 Claude/GPT 模型桶的剩余百分比、配额窗口与重置时间。" +
			"只读网络查询，不写任何文件。接口限流较严，遇 429 自动退避重试（15s/45s/90s，最长约 2.5 分钟）。",
		Annotations: &mcp.ToolAnnotations{
			Title:          "查询 Antigravity 配额",
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in quotaGetIn) (*mcp.CallToolResult, service.Snapshot, error) {
		snap, err := mustSvc().GetQuota(ctx, service.Options{TokenFile: in.TokenFile})
		if err != nil {
			return nil, service.Snapshot{}, err
		}
		return nil, *snap, nil
	})
}

func boolPtr(b bool) *bool { return &b }
