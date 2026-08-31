# mcp-cleanup

清理泄漏的 **MCP server 进程链**（`npx` 拉起的 cmd/node/conhost 树）。
AI 编码工具（ZCode / Claude Code / codex / opencode 等）异常退出或会话结束后，
它们的 MCP server 往往残留不退，越积越多吃内存——本工具负责找出并整链击杀。

## 原理

- 目标：所有 `node.exe + npx` 及 `cmd.exe /c npx` 进程，向上穿透 cmd/node/conhost/npm 包装层找到真正的宿主
- **孤儿链**（宿主进程已死）→ 无论年龄，直接进击杀清单
- **挂靠链**（宿主还在）→ 超过阈值小时数才进击杀清单
- 击杀按链顶 PID 整棵 `taskkill /T /F`，击杀前做快照校验（防 PID 复用误杀）
- 判定逻辑复用实战验证过的 PowerShell 脚本（`go:embed` 打进 exe），Go 只负责嵌入、调用和交互

## 用法

```
mcp-cleanup.exe                # 分析 → 确认 → 击杀 → 前后内存对比
mcp-cleanup.exe -threshold 4   # 闲置阈值改为 4 小时（默认 2）
mcp-cleanup.exe -dry           # 只分析看清单，不击杀
mcp-cleanup.exe -yes           # 跳过确认，供定时任务用
```

## 注意

- 依赖系统自带 Windows PowerShell 5.1，Win10/11 均满足
- exe 未签名且会拉起 powershell，个别杀软可能启发式告警，属正常
- 与 `agent-reaper` 分工：它杀闲置的 **agent 宿主**（按 CPU/IO 活动判闲置），本工具杀 agent 泄漏的 **MCP server 子链**（按年龄 + 孤儿判定），互补不重复
