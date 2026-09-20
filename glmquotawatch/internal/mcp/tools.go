package mcp

import (
	"context"

	"glmquotawatch/internal/service"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- 输入类型 ----
// jsonschema tag 即参数描述，SDK 据此推导 JSON Schema（2020-12），
// MCP 客户端据此渲染表单。可选字段一律 omitempty（不进 required）；
// 存在性/范围等运行时校验在 service 层，Schema 只描述结构。
// 输出直接复用 service 的视图类型（ConfigView/StatusView），保证两侧同形。

type setTokenIn struct {
	Token string `json:"token" jsonschema:"GLM API token；长度须 ≥ 20"`
}

type setConfigIn struct {
	Key   string `json:"key" jsonschema:"配置键：interval（采样间隔，如 90s/5m/1h）、thresholds（逗号分隔，每项 1..99）、hysteresis（0..30）、silent（true/false）"`
	Value string `json:"value" jsonschema:"配置值"`
}

type removedOut struct {
	Removed bool `json:"removed"`
}

// registerTools 注册全部 MCP 工具（与 schema 子命令导出同源）。
// 工具名规则：gqw. + CLI 命令路径（空格换点号），与 CLI 叶命令一一对应，
// 如 token set ↔ gqw.token.set；顶层动词命令 status 无名词段。
// 业务错误直接返回普通 error（*service.Error 的文本即 "code: message"），
// SDK v1.8.0 自动转为 IsError 结果，同时避免零值输出不满足 Schema 校验
// （见《CLI 与 MCP 双壳架构工作指南》4.3）。
func registerTools(s *mcp.Server) {
	ro := func(title string) *mcp.ToolAnnotations {
		return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gqw.status",
		Description: "立即采样一次 GLM 编码套餐用量：各 token 窗口的使用百分比、距下次刷新时间与已告警档位。采样会落历史并推进告警状态，但不发送 Windows 通知",
		Annotations: ro("查询用量"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, service.StatusView, error) {
		svc, err := mustSvc()
		if err != nil {
			return nil, service.StatusView{}, err
		}
		view, _, err := svc.SampleOnce(ctx)
		if err != nil {
			return nil, service.StatusView{}, err
		}
		return nil, view, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gqw.token.set",
		Description: "配置 GLM API token（bigmodel 开放平台密钥，长度 ≥ 20）",
		Annotations: &mcp.ToolAnnotations{Title: "配置 token", OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in setTokenIn) (*mcp.CallToolResult, service.ConfigView, error) {
		svc, err := mustSvc()
		if err != nil {
			return nil, service.ConfigView{}, err
		}
		view, err := svc.SetToken(in.Token)
		if err != nil {
			return nil, service.ConfigView{}, err
		}
		return nil, view, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gqw.token.show",
		Description: "查看已配置的 GLM API token（脱敏）；返回完整配置视图，与 gqw.config.show 同构",
		Annotations: ro("查看 token"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, service.ConfigView, error) {
		svc, err := mustSvc()
		if err != nil {
			return nil, service.ConfigView{}, err
		}
		view, err := svc.GetConfig()
		if err != nil {
			return nil, service.ConfigView{}, err
		}
		return nil, view, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gqw.token.remove",
		Description: "清除已配置的 GLM API token（幂等；常驻监控需停止后重新 start）",
		Annotations: &mcp.ToolAnnotations{Title: "清除 token", DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, removedOut, error) {
		svc, err := mustSvc()
		if err != nil {
			return nil, removedOut{}, err
		}
		if _, err := svc.RemoveToken(); err != nil {
			return nil, removedOut{}, err
		}
		return nil, removedOut{Removed: true}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gqw.config.show",
		Description: "查看当前配置（token 脱敏展示）与数据目录路径",
		Annotations: ro("查看配置"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, service.ConfigView, error) {
		svc, err := mustSvc()
		if err != nil {
			return nil, service.ConfigView{}, err
		}
		view, err := svc.GetConfig()
		if err != nil {
			return nil, service.ConfigView{}, err
		}
		return nil, view, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gqw.config.set",
		Description: "修改单个采样配置键：interval（采样间隔）、thresholds（告警阈值）、hysteresis（滞回）、silent（通知静音）",
		Annotations: &mcp.ToolAnnotations{Title: "修改配置", OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in setConfigIn) (*mcp.CallToolResult, service.ConfigView, error) {
		svc, err := mustSvc()
		if err != nil {
			return nil, service.ConfigView{}, err
		}
		view, err := svc.SetConfig(in.Key, in.Value)
		if err != nil {
			return nil, service.ConfigView{}, err
		}
		return nil, view, nil
	})
}

func boolPtr(b bool) *bool { return &b }
