# Changelog

本文件记录 clictl 的版本变更。格式参考 Keep a Changelog，版本号遵循语义化版本。

## [1.2.0] - 2026-09-12

### Added

- `start` 后台分离启动：DETACHED_PROCESS + CREATE_NEW_PROCESS_GROUP，点火即走立即返回 pid；面向 GUI/托盘/服务类工具（console 程序无输出能力，要看输出用 `run`）；已有活实例时附 `already_running` 提示（不拦截多实例）；未注册/失效 = 127（与 run 一致，错误 JSON 走 stdout）
- `stop` 全量终止：对该工具全部存活后台实例执行 `taskkill /PID x /T /F` 树杀，杀后复探确认并闭环记录（exit_code=1 为强杀约定值，duration 取实测存活时长）；无活实例返回 `already_stopped`，杀失败退出码 1
- PID 探活：OpenProcess 存在性 + GetExitCodeProcess 终止态（STILL_ACTIVE）+ QueryFullProcessImageName 路径比对三重校验，防 PID 回收复用误判；已知局限：退出码恰为 259 的进程误判为存活、SysWOW64 路径重定向误判为已退出（概率极低）
- `list --running`：只列出有后台活实例的工具，附 `running_pids` 与 `last_start`（与 `--status` 互斥）
- `info` 输出新增 `running` 字段（alive + pids），launch 记录新增 `pid` 字段
- launches 表新增 `pid` 列（仅 start 写入）：新库建表自带，存量库启动时自动 ALTER 迁移；SQL 存档见父仓 `docs/go_projects/clictl/migrations/`
- 探活单元测试（真实路径比对 + 已退出进程判死表驱动）；`run`/`start`/`stop` 共用前置校验 lookup（未注册相似名建议三命令一致）
- PowerShell 补全：子命令列表与工具名补全集合纳入 `start`/`stop`

## [1.1.0] - 2026-09-11

### Added

- `run` 未注册名智能建议：前缀 > 子串 > 编辑距离匹配，stderr 错误 JSON 新增可选 `suggestions` 字段（`message` 同步附"是否想找"）
- `completion powershell`：输出 PowerShell Tab 补全脚本（raw 文本，JSON 约定的例外）
- `completion powershell --install` / `--uninstall`：自动写入/移除 `$PROFILE`（标记区块识别，幂等，带 PATH 守卫）
- `completion names`：输出全部工具名供补全脚本消费（raw 每行一个）
- 首批单元测试：`internal/runner/suggest_test.go`（相似名匹配 + 编辑距离）

## [1.0.0] - 2026-09-11

### Added

- `add` / `rm` / `set` / `list` / `info` 管理命令，输出统一 JSON 包络（`--pretty` 缩进模式）
- `run` 透传启动：stdin/stdout/stderr/退出码全部直通，未注册/失效退出 127
- SQLite 存储（`%AppData%\clictl\clictl.db`，WAL + busy_timeout）
- 启动记账：launches 表记录每次启动时间、耗时、退出码；Ctrl+C 不丢记录
- meta 扩展属性：source/tags 白名单校验，序列化后 ≤ 4KB 硬限
- status 实时刷新：list/info/run 触碰时现场 os.Stat 并回写
- `scripts/build.ps1` 构建脚本（-ldflags 版本注入）
- `help` / `-h` / `--help` 帮助命令（JSON 输出）；无参数时输出帮助；子命令级 `-h` 同样生效
