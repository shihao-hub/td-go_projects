# agyquota

**Antigravity 模型配额查询 CLI** —— 不打开 Antigravity IDE，直接查询 Google AI Pro 套餐在 Antigravity 里的模型配额：Gemini 与 Claude/GPT 两个模型桶的 Weekly / Five Hour 剩余百分比和重置时间。

遵循《CLI 工具开发标准》：CLI 人读输出 + `--json` 信封 + `mcp` stdio server + `schema` 契约导出。

## 构建

要求 Go 1.25+：

```powershell
go build -trimpath -ldflags "-s -w -X agyquota/internal/cli.Version=0.1.0" -o agyquota.exe ./cmd/agyquota
```

## 快速上手

```powershell
.\agyquota.exe            # 等价于 agyquota quota
```

输出示例（人读，示意数据）：

```
Antigravity 模型配额（查询于 2026-09-22 02:10:33）
Gemini Models
  gemini-3-pro                weekly      99%  6天21小时后刷新
  gemini-3-pro                5h          97%  2小时2分钟后刷新
Claude and GPT models
  claude-4-5-sonnet           weekly     100%  6天21小时后刷新
```

## 命令一览

| 命令 | 说明 |
|---|---|
| `agyquota` / `agyquota quota` | 查询配额（默认命令） |
| `agyquota quota --json` | JSON 信封输出：`{"ok":true,"data":…}` / `{"ok":false,"error":{…}}` |
| `agyquota quota --raw` | 打印配额接口原始响应（排障用） |
| `agyquota quota --token-file <path>` | 指定凭据文件（缺省见下） |
| `agyquota mcp` | 以 stdio MCP server 运行（工具名 `agyquota.quota.get`） |
| `agyquota schema` | 导出与 tools/list 同源的 MCP 工具契约目录 |
| `--help` / `--version` | 自描述，不触发网络请求 |

退出码：`0` 成功、`1` 业务失败、`2` 参数错误。

## 注意事项

1. **配额接口当前被 Google 软封锁（最重要）**
   Google 对配额接口按"客户端身份头"做校验：本工具与 IDE 使用同一份凭据，但缺少 IDE 特有的客户端身份标识，因此请求一律被 429 拒绝（`rate_limited`）。这是 Google 服务端策略，不是本工具的 bug，本机侧无法绕过。完整调查记录与复活路径见父仓库《设计说明》——工具其余部分全部就绪，封锁解除后无需改代码即可工作。

2. **限流重试会等待，不是卡死**
   遇 429 时自动退避重试（15s → 45s → 90s），进度实时打印到 **stderr**（`[agyquota] 配额接口限流（429），15s 后进行第 1/3 次重试…`）。最坏总耗时约 2.5 分钟；`--json` 模式下进度同样走 stderr，不污染 stdout 的 JSON。

3. **凭据来源与安全**
   凭据只读自 `~\.gemini\antigravity-acp\acp_token.json`——这是 Zed ACP antigravity agent / Antigravity IDE 登录后落盘的 Google OAuth 凭据。文件不存在时先在 IDE 或 Zed ACP 里登录一次。该文件含长期有效的 refresh_token，**注意保密**；本工具只读、绝不回写，进程本身零落盘（无缓存、无日志文件）。

4. **接口是 Google 内部端点，随时可能变化**
   配额接口（`daily-cloudcode-pa.googleapis.com/v1internal:*`）不是公开承诺的 API。若突然持续失败，先跑 `--raw` 看原始响应，再对照《设计说明》的接口地图排查。

5. **不要高频轮询**
   配额接口限流很严，别拿它做秒级监控——那是触发更严限流的最快方式。偶发查询（分钟级间隔）是它的设计场景。

6. **MCP 接入的超时预算**
   `agyquota mcp` 走 stdio，工具 `agyquota.quota.get` 在限流时最长执行约 2.5 分钟，MCP 客户端的调用超时请放宽到 3 分钟以上。MCP 入口不打印进度（stderr 静默），等待属正常。

## 故障排查

| 错误码 | 含义 | 处理 |
|---|---|---|
| `token_file_missing` | 未找到凭据文件 | 在 Antigravity IDE 或 Zed ACP 登录一次，凭据自动落盘 |
| `token_parse_failed` | 凭据缺字段/不可解析 | 重新登录生成凭据 |
| `auth_failed` | OAuth 刷新失败 | refresh_token 已失效，重新登录 |
| `rate_limited` | 429 重试 3 次仍失败 | 关闭正在运行的 IDE 减少竞争；详见注意事项第 1 条 |
| `api_error` | 接口其他失败 | 端点可能变化，对照《设计说明》 |
| `response_parse_failed` | 响应结构变化 | 跑 `--raw` 查看原始响应 |

## 相关文档

- [设计说明（父仓库）](../../../docs/projects/go_projects/agyquota/设计说明.md) —— 配额接口地图、proto 结构、429 调查全过程、复活路径
- [开发计划](../../../docs/projects/go_projects/agyquota/plans/01-agyquota-quota-cli.md)
- 《CLI 工具开发标准》：`docs/projects/go_projects/CLI 工具开发标准.md`

## 依赖

- Go 1.25.x
- github.com/spf13/cobra v1.10.2
- github.com/modelcontextprotocol/go-sdk v1.8.0
