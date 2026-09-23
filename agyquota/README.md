# agyquota

**Antigravity 模型配额查询 CLI** —— 无论是否打开 Antigravity IDE，直接在终端中毫秒级查询 Google AI Pro 在 Antigravity 中的模型配额：Gemini 与 Claude/GPT 两个模型桶的 Weekly / Five Hour 剩余百分比和重置时间。

支持双通道独立查询：
- **`--agy`**：基于官方 **Antigravity CLI (`agy`)** 驱动，查询终端与桌面 GUI 账号配额；执行完毕强制回收进程树，绝无后台残留；
- **`--zed`**：基于 **Zed (`antigravity-acp`)** 独立凭据直连 Google 官方接口，带 50 分钟内存级安全缓存与 1:1 官方 User-Agent 伪装。

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
go build -trimpath -ldflags "-s -w -X agyquota/internal/cli.Version=0.2.0" -o agyquota.exe ./cmd/agyquota
```

## 快速上手

```powershell
# 1. 查询官方 CLI / 桌面端账号配额（主力安全模式，随用随杀）
agyquota --agy

# 2. 查询 Zed 插件账号配额（独立凭据模式，带本地安全缓存）
agyquota --zed
```

输出示例（人读，真实数据）：

```text
Antigravity 模型配额 [Antigravity CLI (agy)]（查询于 2026-09-23 09:55:23）
Gemini Models
  Gemini Models                5h          95%  4小时40分钟后刷新
  Gemini Models                weekly      86%  5天12小时后刷新

Claude and GPT models
  Claude and GPT models        5h         100%  4小时59分钟后刷新
  Claude and GPT models        weekly      96%  5天15小时后刷新
```

## 命令与参数一览

| 命令 / 参数 | 说明 |
|---|---|
| `agyquota --agy` | 查询官方 CLI / 桌面端账号配额（调用 `agy -p /usage`，自动杀进程） |
| `agyquota --zed` | 查询 Zed (`antigravity-acp`) 账号配额（读取本地凭据与缓存） |
| `agyquota --agy --json` | JSON 信封输出：`{"ok":true,"data":…}` / `{"ok":false,"error":{…}}` |
| `agyquota --zed --raw` | 打印底层接口原始响应（排障/调试用） |
| `agyquota mcp` | 以 stdio MCP server 运行（工具名 `agyquota.quota.get`，支持 `source: agy|zed`） |
| `agyquota schema` | 导出与 tools/list 同源的 MCP 工具契约目录 |
| `--help` / `--version` | 自描述，不触发网络请求 |

退出码：`0` 成功、`1` 业务失败、`2` 参数错误（如未传 `--agy`/`--zed`）。

## 架构与安全设计

1. **官方 CLI 安全隔离 (`--agy`)**：
   - 自动探测系统 PATH 或 `%LOCALAPPDATA%\agy\bin\agy.exe`；
   - 运行完成后，无论成功与否均强制终结进程树 (`taskkill /F /T`)，绝不在后台挂起孤儿进程。
2. **Zed 独立接口与防风控设计 (`--zed`)**：
   - 凭据来源：`~/.gemini/antigravity-acp/acp_token.json`；
   - **50 分钟安全缓存**：自动将获取的 Access Token 与过期时间缓存至本项目自有数据目录 `%APPDATA%\language_projects\agyquota\.quota_token_cache.json`（取不到 APPDATA 时回退 `~/.language_projects/agyquota/`），在有效期内直接复用，彻底杜绝高频刷新；凭据目录（`~/.gemini/`）对本工具只读，不写入任何文件；
   - **1:1 官方 User-Agent**：严格复刻 Zed ACP 官方客户端指纹，规避脚本特征识别。
3. **适配器隔离**：业务逻辑收口在 `internal/service`，上层无缝挂接终端人读排版、JSON 格式化输出与 Model Context Protocol (MCP) 接口。

## 故障排查

| 错误码 | 含义 | 处理 |
|---|---|---|
| `source_required` | 未指定 `--agy` 或 `--zed` | 根据要查询的对象选择 `--agy` 或 `--zed` 参数 |
| `agy_not_found` | 未检测到 Antigravity CLI | 确认已安装 `agy` 并在 PATH 或 `%LOCALAPPDATA%\agy\bin\agy.exe` 中 |
| `agy_execute_failed` | `agy -p "/usage"` 执行报错 | 检查网络代理或先独立执行 `agy -p "hello"` 排查 |
| `zed_execute_failed` | Zed 凭据读取或请求失败 | 检查 `~/.gemini/antigravity-acp/acp_token.json` 是否存在 |
| `response_parse_failed` | 输出格式发生变化 | 运行 `--raw` 查看原始响应 |
