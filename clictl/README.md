# clictl

Windows 单文件 CLI 工具注册器/启动器：注册任意 exe，`clictl run <名>` 前台透传启动，`clictl start <名>` 后台分离启动，启动过程被接管并记录。

## 特性

- **管理命令永远输出 JSON**：`{"ok":true,"data":...}` / `{"ok":false,"error":{"code":"...","message":"..."}}`（CLI/GUI 分离思想，未来 GUI/AI 客户端零成本接入）
- **MCP server（`clictl mcp`）**：标准 MCP stdio 协议通道，把注册/启动/执行暴露为带 JSON Schema 的 MCP 工具（`clictl.list` / `clictl.run` 等 10 个），GUI 前端（tooldeck）与 AI 客户端零适配接入；`clictl schema` 可离线导出与 `tools/list` 同源的工具定义。`clictl.run` 为非交互执行：stdin 关闭、输出各保留 1 MiB（超限截断标记）、超时/取消终止整棵进程树（Windows Job Object）
- **前台透传启动（run）**：stdin/stdout/stderr/退出码全部直通子进程，不经任何 shell 包裹，管理数据不混入 stdout；父进程吞掉 Ctrl+C，等子进程退出回写记录后再退，记录不丢
- **后台分离启动（start）**：GUI/托盘/服务类工具点火即走，DETACHED 无控制台不闪黑框，记录 PID 供探活；`stop` 全量树杀
- **PID 探活**：OpenProcess 存在性 + 终止态 + exe 路径比对三重校验（防 PID 回收复用误判）；`list --running` / `info` 现场探活
- **智能提示**：`run`/`start` 未注册名时附相似名建议（前缀/子串/编辑距离）；PowerShell 中 Tab 补全子命令与工具名
- **启动记账**：启动次数、上次启动时间、耗时、退出码、后台实例 PID
- **SQLite 存储**：`%APPDATA%\language_projects\clictl\clictl.db`（取不到 APPDATA 回退 `~/.language_projects/clictl/`），WAL 模式，多实例并发安全

## run vs start

> **要在终端看它输出、等它干完的用 `run`；点火就走的用 `start`。**

| | `run` | `start` |
|---|---|---|
| 语义 | 前台透传，阻塞到子进程退出 | 后台分离（DETACHED），立即返回 |
| IO | stdin/stdout/stderr 直通 | 全部断开（console 程序无输出能力） |
| 退出码 | 透传子进程码 | 0 成功 / 127 未注册或失效 |
| 记账 | started_at + duration + exit_code 闭环 | 只落 started_at + pid；`stop` 杀完闭环（exit_code=1 强杀约定值） |

## 命令

| 命令 | 说明 | 退出码 |
|---|---|---|
| `clictl add <path> [--name N] [--desc D] [--meta JSON]` | 注册 exe；name 默认=文件名去 `.exe` 小写化 | 0 / 1 |
| `clictl rm <name>` | 删除注册（级联删其 launches） | 0 / 1(not_found) |
| `clictl set <name> --meta JSON` | 整体替换 meta（传 `{}` 清空） | 0 / 1 |
| `clictl list [--status active\|invalid] [--running]` | 全部工具，launch_count 降序；`--running` 只看后台活实例（附 running_pids） | 0 |
| `clictl info <name>` | 详情 + 最近 10 条启动 + 累计耗时 + 后台运行状态 | 0 / 1 |
| `clictl run <name> [args...]` | 前台透传启动；未注册时 stderr 错误附相似名 `suggestions` | =子进程码 / 127 |
| `clictl start <name> [args...]` | 后台分离启动，输出 pid；已有活实例时附 `already_running`（不拦截） | 0 / 127 |
| `clictl stop <name>` | 全量终止该工具后台活实例（taskkill /T /F 树杀）并闭环记录 | 0（无活实例 also 0）/ 1（有杀失败） |
| `clictl cp <name> <dest_dir> [--force]` | 复制已注册 exe 到目标目录（目录须已存在）；目标同名文件默认拒绝，需 `--force` 覆盖 | 0 / 1 |
| `clictl completion powershell [--install\|--uninstall]` | PowerShell 补全脚本；`--install` 写入 `$PROFILE`，`--uninstall` 移除 | 0 / 1 |
| `clictl completion names` | 全部工具名，每行一个（供补全脚本消费） | 0 |
| `clictl mcp` | 启动 stdio MCP server（GUI/AI 客户端对话通道；不解析参数，日志走 stderr） | 0 / 1 |
| `clictl schema` | 导出 MCP 工具定义（含 JSON Schema，与 tools/list 同源） | 0 / 1 |
| `clictl --version` | 版本号（JSON） | 0 |
| `clictl help`（或 `-h`/`--help`，或无参数） | 帮助（也是 JSON） | 0 |

- 全局 `--pretty`：缩进 JSON 供人读；全局 `--ascii`：非 ASCII 转义为 `\uXXXX`（PS 5.1 管道等编码不可靠环境用，下游 JSON 解析自动还原）；默认紧凑单行中文原文（注意：`run`/`start` 透传段的 `--pretty`/`--ascii` 属于子进程，不会被 clictl 消费）
- 管理命令错误 JSON 走 stdout；`run` 的前置错误（未注册/文件失效）JSON 走 **stderr**，stdout 只属于子进程；`start`/`stop` 前置错误走 stdout 且保持 127（跨命令一致）
- `completion` 的 `powershell`/`names` 输出 **raw 文本而非 JSON 包络**（消费者是 shell 补全脚本，输出即协议，同 `run` stdout 例外先例）；`--install`/`--uninstall` 为动作型命令仍输出 JSON

## meta 白名单

`--meta` 仅允许两个 key，其余一律拒绝（`meta_unknown_key`）：

```json
{"source": "cargo", "tags": ["dev", "cli"]}
```

- `source`：string，≤ 64 字节，来源标记
- `tags`：[]string，≤ 8 项，每项 ≤ 32 字节，去重后小写存储

## 智能提示

**未注册相似名建议**：`run` 遇到未注册名时，按 前缀 > 子串 > 编辑距离 匹配已注册名，取前 3：

```
> clictl run zedub
{"error":{"code":"not_found","message":"未注册的工具: zedub，是否想找: zedhub","suggestions":["zedhub"]},"ok":false}
```

**PowerShell Tab 补全**（PS 5.1+；clictl 需在 PATH 中）：

```powershell
clictl completion powershell --install    # 一次性安装（写入 $PROFILE 标记区块，幂等）
# 新开 PowerShell 窗口后：
clictl ru<Tab>          # 补全子命令名
clictl run ze<Tab>      # 补全工具名（候选来自注册列表）
clictl run zedhub --fl<Tab>   # 透传段不产生候选
clictl completion powershell --uninstall  # 卸载
```

- 补全器只绑定 `clictl` 命令名，不影响其他工具；内部 try/catch 静默失败，绝不打扰 Tab 体验
- `--install` 写入的安装行带 `Get-Command clictl` 守卫，clictl 不在 PATH 的会话中自动跳过，不污染 shell 启动
- `--install` 安装块自带两行 UTF-8 编码固化（`$OutputEncoding` / `[Console]::OutputEncoding`，修 PS 5.1 原生 exe 间管道中文变 `?`，新会话免 `--ascii`）；旧版 3 行块重跑 `--install` 自动升级为 5 行标准块（返回 `upgraded:true`）；已知副作用：固化后该会话中 ping 等 GBK 老工具输出乱码

## 构建

```powershell
./scripts/build.ps1 -Version 1.0.0   # 产出单文件 clictl.exe，版本号注入
```

依赖 `modernc.org/sqlite`（纯 Go 无 CGO）与 `golang.org/x/sys`（Win32 探活）。完整设计见 `PLAN.md`，使用文档见父仓 `docs/projects/go_projects/clictl/clictl 使用指南.md`，表结构变更存档见父仓 `docs/projects/go_projects/clictl/migrations/`；其他 CLI 项目想复用本 JSON 输出模式，参考父仓 `docs/projects/go_projects/clictl/Go CLI JSON 输出模式参考.md`。
