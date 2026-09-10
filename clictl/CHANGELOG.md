# Changelog

本文件记录 clictl 的版本变更。格式参考 Keep a Changelog，版本号遵循语义化版本。

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
