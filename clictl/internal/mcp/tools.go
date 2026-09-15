package mcp

import (
	"context"
	"encoding/json"

	"clictl/internal/runner"
	"clictl/internal/service"
	"clictl/internal/store"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- 输入/输出类型 ----
// jsonschema tag 即参数描述，SDK 据此推导 JSON Schema（2020-12），
// 前端（tooldeck / MCP 客户端）据此动态渲染表单与结果。
// 可选字段一律 omitempty（不进 required）；存在性/唯一性/字节上限等
// 运行时校验仍在 Go 侧（service 层），Schema 只描述结构。
//
// 注意：JSON 值一律用 any / map[string]any 表达——json.RawMessage 会被
// Schema 推导当成"整数字节数组"，既误导表单渲染又导致输出校验失败。

// toolView store.Tool 的 MCP 视图：meta 从 RawMessage 展开为对象
type toolView struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Path        string         `json:"path"`
	Description string         `json:"description"`
	Status      string         `json:"status"`
	Meta        map[string]any `json:"meta,omitempty"`
	AddedAt     string         `json:"added_at"`
	SizeBytes   int64          `json:"size_bytes"`
	LaunchCount int64          `json:"launch_count"`
	LastLaunch  string         `json:"last_launch,omitempty"`
}

// runningToolView service.RunningTool 的 MCP 视图
type runningToolView struct {
	toolView
	RunningPIDs []int  `json:"running_pids,omitempty"`
	LastStartAt string `json:"last_start,omitempty"`
}

func toolToView(t store.Tool) toolView {
	v := toolView{
		ID:          t.ID,
		Name:        t.Name,
		Path:        t.Path,
		Description: t.Description,
		Status:      t.Status,
		AddedAt:     t.AddedAt,
		SizeBytes:   t.SizeBytes,
		LaunchCount: t.LaunchCount,
		LastLaunch:  t.LastLaunch,
	}
	if len(t.Meta) > 0 {
		var m map[string]any
		if err := json.Unmarshal(t.Meta, &m); err == nil && m != nil {
			v.Meta = m
		}
	}
	return v
}

func runningToView(r service.RunningTool) runningToolView {
	return runningToolView{
		toolView:    toolToView(r.Tool),
		RunningPIDs: r.RunningPIDs,
		LastStartAt: r.LastStartAt,
	}
}

// marshalMeta 把输入的 meta 对象序列化回 service 层的 RawMessage 形态
func marshalMeta(m map[string]any) (json.RawMessage, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

type listIn struct {
	Status  string `json:"status,omitempty" jsonschema:"按状态过滤：active 或 invalid；缺省为全部；与 running 互斥"`
	Running bool   `json:"running,omitempty" jsonschema:"只列出有后台活实例的工具；与 status 互斥"`
}

type listOut struct {
	Tools []runningToolView `json:"tools"` // 统一形状：普通模式附加字段省略
}

type nameIn struct {
	Name string `json:"name" jsonschema:"工具调用名（大小写不敏感）"`
}

type addIn struct {
	Path string         `json:"path" jsonschema:"exe 文件路径；须为 .exe 后缀且文件存在"`
	Name string         `json:"name,omitempty" jsonschema:"调用名；缺省为文件名去 .exe 后小写化"`
	Desc string         `json:"desc,omitempty" jsonschema:"描述"`
	Meta map[string]any `json:"meta,omitempty" jsonschema:"扩展属性对象；仅允许 source 与 tags 两个 key，传 {} 等价于不设置"`
}

type setIn struct {
	Name string         `json:"name" jsonschema:"工具调用名（大小写不敏感）"`
	Meta map[string]any `json:"meta" jsonschema:"整体替换后的扩展属性对象；传 {} 清空；仅允许 source 与 tags"`
}

type startIn struct {
	Name string   `json:"name" jsonschema:"工具调用名（大小写不敏感）"`
	Args []string `json:"args,omitempty" jsonschema:"传给子进程的参数（数组直传，不经 shell 包裹）"`
}

type cpIn struct {
	Name    string `json:"name" jsonschema:"工具调用名（大小写不敏感）"`
	DestDir string `json:"dest_dir" jsonschema:"目标目录路径（必须已存在，不自动创建）"`
	Force   bool   `json:"force,omitempty" jsonschema:"目标已存在同名文件时强制覆盖；源与目标是同一文件时永远拒绝"`
}

type runIn struct {
	Name      string   `json:"name" jsonschema:"工具调用名（大小写不敏感）"`
	Args      []string `json:"args,omitempty" jsonschema:"传给子进程的参数（数组直传，不经 shell 包裹）"`
	TimeoutMs int64    `json:"timeout_ms,omitempty" jsonschema:"超时毫秒数，超时终止整棵进程树；缺省 60000；小于等于 0 按 60000 处理"`
}

type infoOut struct {
	Tool            toolView             `json:"tool"`
	RecentLaunches  []store.Launch       `json:"recent_launches"`
	FinishedCount   int64                `json:"finished_count"`
	TotalDurationMs int64                `json:"total_duration_ms"`
	Running         service.RunningState `json:"running"`
}

type versionOut struct {
	Version string `json:"version"`
}

// registerTools 注册全部 MCP 工具（与 clictl schema 导出同源）
func registerTools(s *mcp.Server) {
	ro := func(title string) *mcp.ToolAnnotations {
		return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clictl.list",
		Description: "列出已注册的工具（按启动次数降序）；可选按状态过滤或只看有后台活实例的",
		Annotations: ro("工具列表"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, listOut, error) {
		svc, err := mustSvc()
		if err != nil {
			return errResult(err), listOut{}, nil
		}
		out := listOut{}
		if in.Running {
			if in.Status != "" {
				return errResult(&service.Error{Code: "bad_args", Message: "running 与 status 互斥"}), listOut{}, nil
			}
			tools, err := svc.ListRunning()
			if err != nil {
				return errResult(err), listOut{}, nil
			}
			out.Tools = make([]runningToolView, 0, len(tools))
			for _, t := range tools {
				out.Tools = append(out.Tools, runningToView(t))
			}
			return nil, out, nil
		}
		tools, err := svc.ListTools(in.Status)
		if err != nil {
			return errResult(err), listOut{}, nil
		}
		out.Tools = make([]runningToolView, 0, len(tools))
		for _, t := range tools {
			out.Tools = append(out.Tools, runningToolView{toolView: toolToView(t)})
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clictl.info",
		Description: "查看单个工具详情：注册信息 + 最近 10 条启动记录 + 累计耗时 + 后台运行状态",
		Annotations: ro("工具详情"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, *infoOut, error) {
		svc, err := mustSvc()
		if err != nil {
			return errResult(err), nil, nil
		}
		res, err := svc.Info(in.Name)
		if err != nil {
			return errResult(err), nil, nil
		}
		return nil, &infoOut{
			Tool:            toolToView(res.Tool),
			RecentLaunches:  res.RecentLaunches,
			FinishedCount:   res.FinishedCount,
			TotalDurationMs: res.TotalDurationMs,
			Running:         res.Running,
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clictl.add",
		Description: "注册一个 exe 工具；名字缺省取文件名去 .exe 小写化",
		Annotations: &mcp.ToolAnnotations{Title: "注册工具", OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, toolView, error) {
		svc, err := mustSvc()
		if err != nil {
			return errResult(err), toolView{}, nil
		}
		raw, err := marshalMeta(in.Meta)
		if err != nil {
			return errResult(&service.Error{Code: "bad_args", Message: "meta 序列化失败: " + err.Error()}), toolView{}, nil
		}
		tool, err := svc.Add(in.Path, in.Name, in.Desc, raw)
		if err != nil {
			return errResult(err), toolView{}, nil
		}
		return nil, toolToView(tool), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clictl.set",
		Description: "整体替换工具的扩展属性 meta（传 {} 清空）；仅允许 source 与 tags 两个 key",
		Annotations: &mcp.ToolAnnotations{Title: "更新 meta", DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in setIn) (*mcp.CallToolResult, toolView, error) {
		svc, err := mustSvc()
		if err != nil {
			return errResult(err), toolView{}, nil
		}
		raw, err := marshalMeta(in.Meta)
		if err != nil {
			return errResult(&service.Error{Code: "bad_args", Message: "meta 序列化失败: " + err.Error()}), toolView{}, nil
		}
		tool, err := svc.SetMeta(in.Name, raw)
		if err != nil {
			return errResult(err), toolView{}, nil
		}
		return nil, toolToView(tool), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clictl.rm",
		Description: "删除工具注册（级联删除其启动记录）",
		Annotations: &mcp.ToolAnnotations{Title: "删除注册", DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, *service.RemoveResult, error) {
		svc, err := mustSvc()
		if err != nil {
			return errResult(err), nil, nil
		}
		res, err := svc.Remove(in.Name)
		if err != nil {
			return errResult(err), nil, nil
		}
		return nil, res, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clictl.start",
		Description: "后台分离启动工具（DETACHED，点火即走），返回 pid；适合 GUI/托盘/服务类程序",
		Annotations: &mcp.ToolAnnotations{Title: "后台启动", OpenWorldHint: boolPtr(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in startIn) (*mcp.CallToolResult, runner.StartResult, error) {
		svc, err := mustSvc()
		if err != nil {
			return errResult(err), runner.StartResult{}, nil
		}
		res, err := svc.Start(in.Name, in.Args)
		if err != nil {
			return errResult(err), runner.StartResult{}, nil
		}
		return nil, res, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clictl.stop",
		Description: "终止该工具全部后台活实例（树杀）并闭环启动记录；无活实例时幂等返回",
		Annotations: &mcp.ToolAnnotations{Title: "停止后台实例", DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, runner.StopResult, error) {
		svc, err := mustSvc()
		if err != nil {
			return errResult(err), runner.StopResult{}, nil
		}
		res, err := svc.Stop(in.Name)
		if err != nil {
			return errResult(err), runner.StopResult{}, nil
		}
		// 部分失败：结果仍完整返回，但标记失败供客户端提示
		if len(res.Failed) > 0 {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "部分后台实例终止失败（杀后复探仍存活），详见 failed 列表"}},
			}, res, nil
		}
		return nil, res, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clictl.cp",
		Description: "复制已注册的 exe 到目标目录（目录须已存在；目标同名文件默认拒绝，需 force 覆盖）",
		Annotations: &mcp.ToolAnnotations{Title: "复制文件", DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in cpIn) (*mcp.CallToolResult, *service.CpResult, error) {
		svc, err := mustSvc()
		if err != nil {
			return errResult(err), nil, nil
		}
		res, err := svc.Cp(in.Name, in.DestDir, in.Force)
		if err != nil {
			return errResult(err), nil, nil
		}
		return nil, res, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clictl.run",
		Description: "非交互前台执行工具：收集 stdout/stderr 与退出码后返回（不透传 stdio）。超时或取消会终止整棵进程树。要看输出、等结果但不需要交互 stdin 的场景用它",
		Annotations: &mcp.ToolAnnotations{Title: "前台执行", OpenWorldHint: boolPtr(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in runIn) (*mcp.CallToolResult, runOut, error) {
		svc, err := mustSvc()
		if err != nil {
			return errResult(err), runOut{}, nil
		}
		out, err := execTool(ctx, svc, in.Name, in.Args, in.TimeoutMs)
		if err != nil {
			return errResult(err), runOut{}, nil
		}
		// 非零退出/超时/取消：结构化结果照常返回，同时标记失败供客户端提示
		if out.ExitCode != 0 || out.TimedOut || out.Cancelled {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: out.failureSummary()}},
			}, out, nil
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clictl.version",
		Description: "查看 clictl 版本号",
		Annotations: ro("版本"),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, versionOut, error) {
		return nil, versionOut{Version: Version}, nil
	})
}

func boolPtr(b bool) *bool { return &b }
