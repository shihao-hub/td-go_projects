# glmquotawatch

定时采样智谱 GLM（bigmodel）编码套餐用量，跨越阈值（默认 50/60/80/90%）时弹 Windows Toast 通知的 CLI 工具。MCP/CLI 双壳：既有面向人的命令行，也有供 AI 客户端接入的 stdio MCP server。

## 构建

```powershell
cd go_projects\glmquotawatch
go build -ldflags "-X glmquotawatch/internal/cli.Version=v0.1.0" -o glmquotawatch.exe .
```

exe 默认带站标地鼠图标：项目根下的 `icon.ico` 与 `rsrc_windows_amd64.syso` 由 `go build` 自动链接，无需额外参数；更换图标时用 rsrc 重出 syso（见父仓 `docs/projects/go_projects/GUIDE-GO-EXE-ICON.md`）。

## 快速上手

```powershell
.\glmquotawatch.exe token set <你的GLM_API_TOKEN>   # bigmodel 开放平台密钥
.\glmquotawatch.exe status                          # 立即采样一次
.\glmquotawatch.exe start                           # 启动常驻监控（Ctrl+C 停止）
```

## 命令

| 命令 | 说明 |
|---|---|
| `token set <token>` / `show` / `remove` | 配置 / 脱敏查看 / 清除 token |
| `status [--json]` | 立即采样一次（落历史并推进告警状态，不发通知） |
| `config show` / `config set <key> <value>` | 查看 / 修改配置（`interval`、`thresholds`、`hysteresis`、`silent`） |
| `start` | 常驻监控：定时采样 + 跨阈值弹 Toast；单实例锁；interval 热加载 |
| `mcp` | 以 stdio MCP server 运行（工具：`gqw.status`、`gqw.token.set/show/remove`、`gqw.config.show/set`，与 CLI 叶命令一一对应） |
| `schema` | 导出与 `tools/list` 同源的 MCP 工具定义 |

## 数据文件

全部位于 `%APPDATA%\language_projects\glmquotawatch\`：`config.json`（配置，token 明文保存于本机）、`state.json`（告警武装状态，重启不重复轰炸）、`samples-YYYY-MM.jsonl`（采样历史，供二期突发检测）、`daemon.lock`（单实例锁）。

## 通知不弹排查

1. 先确认日志有 `通知已发送`——有这行说明程序侧成功，是系统侧丢弃；
2. Windows 设置 → 系统 → 通知：总开关 + 「Windows PowerShell」的通知权限（通知以 PowerShell 的 AUMID 身份发出）；
3. 勿扰 / 专注助手是否开启；
4. 日志都没有：`powershell` 是否在 PATH。

## 已知限制

- token 在本机 config.json 中明文保存（与仓内 quickask 同做法）；
- daemon.lock 按 PID 存活判断，极端 PID 复用会误判（删除 lock 文件即可恢复）；
- 突发消耗检测、run-once + 计划任务模式、周额度套餐适配、TIME_LIMIT（MCP 月度）告警留二期。
