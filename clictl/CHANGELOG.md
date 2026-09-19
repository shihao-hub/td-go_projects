# Changelog

本文件记录 clictl 的版本变更。格式参考 Keep a Changelog，版本号遵循语义化版本。

## [1.4.0] - 2026-09-13

### Added

- `--ascii` 全局开关：JSON 输出中非 ASCII 字符转义为 `\uXXXX`（BMP 外拆代理对，小写十六进制对齐 Go json 风格），产物全 ASCII 免疫 PS 5.1 管道重编码（`$OutputEncoding` 默认 ASCII 是中文变 `?` 的元凶），下游 JSON 解析自动还原；位置规则同 `--pretty`（`run`/`start` 透传段除外，透传段的 `--ascii` 属于子进程）；stdout 与 stderr 两个 JSON 出口在 marshal 单点同时覆盖；默认关闭，不加开关输出仍为 UTF-8 原文
- `completion powershell --install` 安装块固化编码：新增 `$OutputEncoding` 与 `[Console]::OutputEncoding` 两行 UTF8 设置（位于安装行之前、Get-Command 守卫之外无条件执行），新开 PS 5.1 会话中原生 exe 间管道免 `--ascii` 直接可用
- 旧版 3 行安装块重跑 `--install` 自动升级为 5 行标准块（返回 `upgraded:true`），免卸载重装；标记不完整时保守不动；`--uninstall` removed 计数 3→5
- 已知副作用：profile 固化后 `[Console]::OutputEncoding=UTF8` 会使 ping 等 GBK 老工具在该会话输出乱码（仅该会话）
- 单元测试：escapeNonASCII 边界（纯 ASCII 快扫 / 中文 / 代理对 / U+007F 不转 / U+0080 / U+FFFF / U+10000 / 无效字节）、marshal 集成（默认中文原样、Ascii 全 ASCII 且 Unmarshal 还原、Pretty+Ascii 组合）、stripGlobalFlags 剥离与置位

## [1.3.0] - 2026-09-12

### Added

- `cp` 复制命令：把已注册 exe 复制到指定目录（`clictl cp <name> <dest_dir>`），保留原文件名；目标目录必须已存在（不自动创建，缺失报 `dest_not_found`）；目标同名文件默认拒绝（`dest_exists`），`--force` 强制覆盖；源与目标为同一文件时始终拒绝（`same_path`，防截断损坏）；流式复制不整读内存
- PowerShell 补全：子命令列表与工具名补全集合纳入 `cp`

## [1.2.0] - 2026-09-12

### Added

- `start` 后台分离启动：DETACHED_PROCESS + CREATE_NEW_PROCESS_GROUP，点火即走立即返回 pid；面向 GUI/托盘/服务类工具（console 程序无输出能力，要看输出用 `run`）；已有活实例时附 `already_running` 提示（不拦截多实例）；未注册/失效 = 127（与 run 一致，错误 JSON 走 stdout）
- `stop` 全量终止：对该工具全部存活后台实例执行 `taskkill /PID x /T /F` 树杀，杀后复探确认并闭环记录（exit_code=1 为强杀约定值，duration 取实测存活时长）；无活实例返回 `already_stopped`，杀失败退出码 1
- PID 探活：OpenProcess 存在性 + GetExitCodeProcess 终止态（STILL_ACTIVE）+ QueryFullProcessImageName 路径比对三重校验，防 PID 回收复用误判；已知局限：退出码恰为 259 的进程误判为存活、SysWOW64 路径重定向误判为已退出（概率极低）
- `list --running`：只列出有后台活实例的工具，附 `running_pids` 与 `last_start`（与 `--status` 互斥）
- `info` 输出新增 `running` 字段（alive + pids），launch 记录新增 `pid` 字段
- launches 表新增 `pid` 列（仅 start 写入）：新库建表自带，存量库启动时自动 ALTER 迁移；SQL 存档见父仓 `docs/projects/go_projects/clictl/migrations/`
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
