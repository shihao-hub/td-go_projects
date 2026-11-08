# rebootremind

Windows 开机时长与内存健康巡检重启提醒工具。

## 背景

长期不重启时，同样的软件组合内存占用会从正常的 70%~80% 慢性膨胀到 90%+；重启能清空内核驱动非分页池与 V8 堆碎片。但「刚开机就开大项目」也会让内存短暂冲高，因此本工具把**开机时长作为硬前置条件**，避免高负载误报。

设计文档：<../../docs/projects/go_projects/rebootremind/plans/01-rebootremind-plan.md>

## 判定规则

按以下顺序判定，前一条命中即返回：

1. 处于 Snooze（延迟提醒）冷却期内 → 静默；
2. 开机时长 < `min_uptime_hours`（默认 72 小时）→ 静默，杜绝刚开机高负载误报；
3. 开机时长 ≥ `force_uptime_hours`（默认 168 小时）→ 无条件温和提醒；
4. 开机时长达标且内存 ≥ `min_memory_percent`（默认 85%）→ 提醒；
5. 其余情况 → 静默。

命中后弹出 Windows 原生 `TaskDialogIndirect` 弹窗（不点不消失、强制可见），展示开机时长、内存占用与 Top 5 内存进程，并提供：

- `4 小时后再提醒我`（Snooze 4h）
- `明天再提醒我`（Snooze 24h）
- `立即重启系统`（二次确认后执行 `shutdown /r /t 0`）
- 右上角关闭（视同 Snooze 4h）

## 能力表

| 业务用例 | CLI 入口 | MCP 工具 | 输入、结果与副作用 | 暴露或不暴露的理由 |
| :--- | :--- | :--- | :--- | :--- |
| 查询健康状态 | `status` | 不暴露 | 输出开机时长、内存、Top 5 进程、阈值、Snooze 与判定 | 本工具是计划任务驱动的本机巡检器，无长期管理会话；`--json` 已可供脚本/Agent 调用 |
| 执行巡检 | `check` | 不暴露 | 采集快照并判定；`--notify` 命中后弹窗并写 Snooze；`--force` 跳过阈值 | 弹窗必须由当前登录用户交互完成，不适合无状态工具调用 |
| 计划任务管理 | `task install/uninstall/status` | 不暴露 | 注册/删除/查询 `rebootremind_check`（每小时一次） | 属于本机安装运维动作，由用户显式执行 |
| 配置管理 | `config show/set/path` | 不暴露 | 读写 `%APPDATA%\language_projects\rebootremind\<dev|prod>\config.json` | 同上 |
| 契约导出 | `schema` | 不暴露 | 零 I/O 输出 CLI 静态契约（`interface: "cli"`） | 供自动化与 Agent 发现命令 |

> 按《CLI 工具开发标准》§0.1，不提供 MCP 的理由已在此登记：本工具没有「跨请求管理状态」的业务面，所有能力均是一次性巡检/安装动作，`--json` + `schema` 已覆盖机器调用需求。

## 安装与使用

构建：

```powershell
cd D:\Users\language_projects\go_projects\rebootremind
uv run scripts/build.py --dev     # 产物 bin\rebootremind-dev.exe
uv run scripts/build.py --release # 仅 HEAD 命中 rebootremind/v* tag 且工作区干净时允许
```

注册每小时巡检（建议使用正式版或已构建的 dev 产物）：

```powershell
.\bin\rebootremind-dev.exe task install
.\bin\rebootremind-dev.exe task status
.\bin\rebootremind-dev.exe task uninstall
```

`task install` 拒绝把 `go run` 的临时构建路径写入计划任务；确需强行注册时加 `--force`。

常用命令：

```powershell
.\bin\rebootremind-dev.exe status
.\bin\rebootremind-dev.exe status --json
.\bin\rebootremind-dev.exe check              # 只判定，不弹窗
.\bin\rebootremind-dev.exe check --force --notify  # 强制弹窗做交互验证
.\bin\rebootremind-dev.exe config show
.\bin\rebootremind-dev.exe config set min_memory_percent 80
.\bin\rebootremind-dev.exe schema
.\bin\rebootremind-dev.exe version
```

## 配置项

配置文件：`%APPDATA%\language_projects\rebootremind\<dev|prod>\config.json`（不存在时使用默认值，不主动写盘）。

| 键 | 默认值 | 含义 |
| :--- | ---: | :--- |
| `min_uptime_hours` | 72 | 触发提醒的最低开机时长（小时） |
| `min_memory_percent` | 85 | 触发提醒的最低内存占用（%） |
| `force_uptime_hours` | 168 | 无条件提醒的开机时长（小时） |
| `snooze_short_hours` | 4 | 「稍后提醒」的冷却时长 |
| `snooze_long_hours` | 24 | 「明天提醒」的冷却时长 |

约束：`force_uptime_hours ≥ min_uptime_hours`、`snooze_long_hours ≥ snooze_short_hours`，否则 `config set` 直接拒绝。

## 数据与状态

- 数据目录按构建形态隔离：dev → `...\rebootremind\dev`，release → `...\rebootremind\prod`；
- `state.json` 保存 `snooze_until`、`last_notified_at`、`last_action`，原子写入；
- `check.lock` 防止计划任务与手动巡检并发弹窗，锁超过 2 分钟视为陈旧并自动清理。

## 构建产物

| 通道 | 产物 | 说明 |
| :--- | :--- | :--- |
| dev | `bin/rebootremind-dev.exe` | 版本带 `-dev`，数据目录 `dev` |
| release | `bin/rebootremind.exe` | 版本为干净 tag，数据目录 `prod` |

PE 资源由 `rsrc` 注入：地鼠图标 + manifest（启用 Common Controls v6，TaskDialog 必需）。图标源为 `docs/assets/projects/go_projects/go-default.ico`。

## 验收记录（2026-11-07）

- `go build ./...`、`go vet ./...`、`go test ./...` 全部通过；
- `status` 人读与 `--json` 输出正常，Top 5 进程聚合符合预期；
- `schema` 可被 JSON 解析，`interface: "cli"`、9 个命令条目；
- `config set` 生效并拒绝非法值（退出码 2）；
- `task install/status/uninstall` 真机验证通过，`next_run` 能正确解析；含空格的 exe 路径同样可注册；
- `check --force --notify` 端到端验证：弹窗成功弹出，通过 `TDM_CLICK_BUTTON` 模拟点击「4 小时后再提醒我」，进程正常退出且 `state.json` 写入 `snooze_until` 与 `last_action=snooze_short`（验证后已清理测试状态）；
- PE 图标验证输出 `32x32`。

## 已知限制

- 仅支持 Windows；
- Top 进程按 exe 名聚合；无权限读取的进程会被跳过，因此百分比总和可能小于内存总占用；
- 计划任务默认为当前用户登录时运行（弹窗需要交互桌面）；
- 弹窗是模态交互，自动化验证通过 TaskDialog 消息模拟点击；真实鼠标点击路径建议首次使用时人工过一遍。
