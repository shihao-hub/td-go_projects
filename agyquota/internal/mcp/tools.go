package mcp

import (
	"context"
	"fmt"

	"agyquota/internal/api"
	"agyquota/internal/client"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- 输入类型 ----
// jsonschema tag 即参数描述，SDK 据此推导 JSON Schema（2020-12）。
// 可选字段一律 omitempty（不进 required）；业务校验在 daemon 内。
// 输出复用 api.Snapshot 类型，保证 CLI/MCP 两侧同形。

type quotaGetIn struct {
	Source    string `json:"source,omitempty" jsonschema:"可选：查询数据源，'agy' 为官方 CLI/桌面端（默认），'zed' 为 Zed 编辑器插件账号"`
	TokenFile string `json:"tokenFile,omitempty" jsonschema:"可选：凭据文件路径（仅 zed 模式生效；缺省为 ~/.gemini/antigravity-acp/acp_token.json）"`
}

// quotaGetSchema 显式构造输入 schema 并保留 source 的枚举约束。
// jsonschema-go v0.4 的 struct tag 已不支持 enum 关键字（出现 WORD= 前缀会
// panic），因此按 SDK 推荐方式定制 schema，保持与 0.2.3 契约一致的枚举。
func quotaGetSchema() (*jsonschema.Schema, error) {
	s, err := jsonschema.For[quotaGetIn](&jsonschema.ForOptions{})
	if err != nil {
		return nil, err
	}
	if prop, ok := s.Properties["source"]; ok && prop != nil {
		prop.Enum = []any{api.SourceAgy, api.SourceZed}
	}
	return s, nil
}

// registerTools 注册全部 MCP 工具（与 schema 子命令导出同源）。
// 工具名规则：agyquota.<资源>.<动词> 三段式，与 CLI 命令路径对应
// （quota → agyquota.quota.get）。业务错误返回普通 error
// （*api.Error 文本即 message），SDK 自动转 IsError 结果。
func registerTools(s *mcp.Server, cfg client.Config) {
	schema, err := quotaGetSchema()
	if err != nil {
		panic(fmt.Errorf("构建 agyquota.quota.get 输入 schema 失败: %w", err))
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "agyquota.quota.get",
		Description: "查询 Google Antigravity 模型配额：Gemini 与 Claude/GPT 模型桶的剩余百分比、配额窗口与重置时间。" +
			"支持通过 'source' 参数指定 'agy'（官方终端/桌面端）或 'zed'（Zed 编辑器插件）。",
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{
			Title:          "查询 Antigravity 配额",
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in quotaGetIn) (*mcp.CallToolResult, api.Snapshot, error) {
		src := in.Source
		if src == "" {
			src = api.SourceAgy
		}
		cl := mustClient(cfg)
		if err := cl.EnsureDaemon(ctx); err != nil {
			return nil, api.Snapshot{}, err
		}
		snap, err := cl.GetQuota(ctx, api.QuotaRequest{
			Source:    src,
			TokenFile: in.TokenFile,
		}, nil)
		if err != nil {
			return nil, api.Snapshot{}, err
		}
		return nil, *snap, nil
	})
}

func boolPtr(b bool) *bool { return &b }
