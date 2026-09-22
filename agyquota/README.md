# agyquota

**Antigravity 模型配额查询 CLI** —— 无论是否打开 Antigravity IDE，直接在终端中毫秒级查询 Google AI Pro 在 Antigravity 中的模型配额：Gemini 与 Claude/GPT 两个模型桶的 Weekly / Five Hour 剩余百分比和重置时间。

本项目已重构为基于官方 **Antigravity CLI (`agy`)** 作为数据驱动源，彻底解决早期直连底层接口时的 429 客户端校验封锁问题。

遵循《CLI 工具开发标准》：CLI 人读输出 + `--json` 信封 + `mcp` stdio server + `schema` 契约导出。

## 构建与安装

要求 Go 1.25+：

### 1. 使用 scripts 构建脚本（推荐）

```powershell
# 构建 agyquota.exe
.\scripts\build.ps1

# 构建并自动安装部署至系统 PATH (~/.local/bin)
.\scripts\install.ps1
```

### 2. 手动构建

```powershell
go build -trimpath -ldflags "-s -w -X agyquota/internal/cli.Version=0.1.0" -o agyquota.exe ./cmd/agyquota
```

## 快速上手

```powershell
agyquota            # 全局已安装时，等价于 agyquota quota
```

输出示例（人读，真实数据）：

```
Antigravity 模型配额（查询于 2026-09-23 00:25:55）
Gemini Models
  Gemini Models                5h          84%  2小时40分钟后刷新
  Gemini Models                weekly      91%  5天21小时后刷新

Claude and GPT models
  Claude and GPT models        5h         100%  4小时59分钟后刷新
  Claude and GPT models        weekly      96%  6天后刷新
```

## 命令一览

| 命令 | 说明 |
|---|---|
| `agyquota` / `agyquota quota` | 查询配额（默认命令，自动调用 `agy`） |
| `agyquota quota --json` | JSON 信封输出：`{"ok":true,"data":…}` / `{"ok":false,"error":{…}}` |
| `agyquota quota --raw` | 打印底层接口/agy 原始响应（排障用） |
| `agyquota mcp` | 以 stdio MCP server 运行（工具名 `agyquota.quota.get`） |
| `agyquota schema` | 导出与 tools/list 同源的 MCP 工具契约目录 |
| `--help` / `--version` | 自描述，不触发网络请求 |

退出码：`0` 成功、`1` 业务失败、`2` 参数错误。

## 架构与工作原理

1. **底层引擎**：通过本地系统的 `agy`（自动探测 PATH 或 `%LOCALAPPDATA%\agy\bin\agy.exe`）执行 `/usage` 命令获取官方权威数据。
2. **零凭证管理心智负担**：完全复用 `agy` 已有的凭据体系（`~/.gemini/antigravity-acp/acp_token.json`）。
3. **适配器隔离**：业务逻辑收口在 `internal/service`，上层无缝挂接终端人读排版、JSON 格式化输出与 Model Context Protocol (MCP) 接口。

## 故障排查

| 错误码 | 含义 | 处理 |
|---|---|---|
| `agy_not_found` | 未检测到 Antigravity CLI | 确认已安装 `agy` 并在 PATH 或 `%LOCALAPPDATA%\agy\bin\agy.exe` 中 |
| `agy_execute_failed` | `agy -p "/usage"` 执行报错 | 检查网络代理或先独立执行 `agy -p "hello"` 排查 |
| `response_parse_failed` | agy 输出格式发生变化 | 运行 `--raw` 查看原始响应 |

## 依赖

- Go 1.25.x
- github.com/spf13/cobra v1.10.2
- github.com/modelcontextprotocol/go-sdk v1.8.0
- 本机已安装 Antigravity CLI (`agy`)
