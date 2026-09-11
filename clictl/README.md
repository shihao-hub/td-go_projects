# clictl

Windows 单文件 CLI 工具注册器/启动器：注册任意 exe，`clictl run <名>` 透传启动，启动过程被接管并记录。

## 特性

- **管理命令永远输出 JSON**：`{"ok":true,"data":...}` / `{"ok":false,"error":{"code":"...","message":"..."}}`（CLI/GUI 分离思想，未来 GUI/AI 客户端零成本接入）
- **透传启动**：`clictl run` 的 stdin/stdout/stderr/退出码全部直通子进程，不经任何 shell 包裹，管理数据不混入 stdout
- **启动记账**：启动次数、上次启动时间、耗时、退出码；父进程吞掉 Ctrl+C，等子进程退出回写记录后再退，记录不丢
- **SQLite 存储**：`%AppData%\clictl\clictl.db`（取不到回退 `~/.clictl/`），WAL 模式，多实例并发安全

## 命令

| 命令 | 说明 | 退出码 |
|---|---|---|
| `clictl add <path> [--name N] [--desc D] [--meta JSON]` | 注册 exe；name 默认=文件名去 `.exe` 小写化 | 0 / 1 |
| `clictl rm <name>` | 删除注册（级联删其 launches） | 0 / 1(not_found) |
| `clictl set <name> --meta JSON` | 整体替换 meta（传 `{}` 清空） | 0 / 1 |
| `clictl list [--status active\|invalid]` | 全部工具，launch_count 降序 | 0 |
| `clictl info <name>` | 详情 + 最近 10 条启动 + 累计耗时 | 0 / 1 |
| `clictl run <name> [args...]` | 透传启动 | =子进程码 / 127 |
| `clictl --version` | 版本号（JSON） | 0 |
| `clictl help`（或 `-h`/`--help`，或无参数） | 帮助（也是 JSON） | 0 |

- 全局 `--pretty`：缩进 JSON 供人读；默认紧凑单行（注意：`run` 透传段的 `--pretty` 属于子进程，不会被 clictl 消费）
- 管理命令错误 JSON 走 stdout；`run` 的前置错误（未注册/文件失效）JSON 走 **stderr**，stdout 只属于子进程

## meta 白名单

`--meta` 仅允许两个 key，其余一律拒绝（`meta_unknown_key`）：

```json
{"source": "cargo", "tags": ["dev", "cli"]}
```

- `source`：string，≤ 64 字节，来源标记
- `tags`：[]string，≤ 8 项，每项 ≤ 32 字节，去重后小写存储

## 构建

```powershell
./scripts/build.ps1 -Version 1.0.0   # 产出单文件 clictl.exe，版本号注入
```

依赖 `modernc.org/sqlite`（纯 Go 无 CGO）。完整设计见 `PLAN.md`，使用文档见父仓 `docs/go_projects/clictl/clictl.md`；其他 CLI 项目想复用本 JSON 输出模式，参考父仓 `docs/go_projects/clictl/cli-json-pattern.md`。
