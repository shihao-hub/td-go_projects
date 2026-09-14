# filesync

带忽略规则的本地目录同步 CLI（A → B 镜像：复制新增/修改、确认删除多余）。

从 `file-sync-native`（Wails GUI 版）复制改造而来，`engine/`、`ignore/`、`config/`、`models/`、`logging/` 核心引擎原样继承，WebView 前端替换为子命令 + JSON 输出（模式参考 `docs/go_projects/clictl/Go CLI JSON 输出模式参考.md`）。

## 命令

```
filesync add <name> <source> <target> [--rule R]...   # 注册任务（--rule 忽略规则可多次，gitignore 风格后规则胜出）
filesync list                                          # 全部任务
filesync run <name> [--dry-run] [--force] [--yes]      # 执行同步
filesync remove <name>                                 # 删除任务
filesync help                                          # JSON 帮助
```

## 行为说明

- **数据目录**：任务配置 `config.json`、哈希缓存 `hash-cache.gob`、日志 `filesync.log` 统一存于 `%APPDATA%\language_projects\filesync\`（取不到 APPDATA 回退 `~/.language_projects/filesync/`），与已归档 GUI 版的 `~/.file-sync/` 不互通
- **三级判定快速同步**：目标缺失/size 不同 → 复制；size+mtime 同 → 零读取跳过；size 同 mtime 异 → 双侧哈希比对（缓存感知）。`--force` 全量内容校验
- **删除默认拒绝**（脚本安全）：待删清单见结果 `diff.deleted`，确认后 `--yes` 执行；`--dry-run` 只算差异不落盘
- **进度走 stderr**（单行刷新），stdout 只输出最终 JSON 结果；`--pretty` 缩进供人读
- Ctrl+C 随时取消；单文件错误不中断整体，汇总在 `errors`
- watch（持续监听）模式暂未实现

## 构建

```
go build ./cmd/filesync
```

## 与 file-sync-native 的关系

file-sync-native（Wails GUI 版）已归档至父仓 `.archived/go_projects/file-sync-native`，由本项目替代；数据文件已按仓库规范迁至 `%APPDATA%\language_projects\filesync\`，与 GUI 版 `~/.file-sync/` 不再互通（旧数据可自行拷贝迁移）。
