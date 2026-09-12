# zreadmanager

zread browse 的命令行生命周期管理器：后台启动 / 树杀停止 / 状态探活，无托盘无窗口。

从 `zread-tray`（系统托盘版）复制改造而来，进程管理逻辑（browse 子进程拉起、taskkill 树杀、记住上次工作区）原样继承，托盘/目录选择框替换为子命令与 JSON 输出（模式参考 `docs/go_projects/clictl/Go CLI JSON 输出模式参考.md`）。

## 命令

```
zreadmanager start [--dir D] [--host H] [--port P] [--generate]   # 后台启动并记住工作区
zreadmanager stop                                                 # 树杀活实例（幂等）
zreadmanager restart                                              # 用上次参数重启
zreadmanager status                                               # 运行状态（PID + 创建时间探活）
zreadmanager help                                                 # JSON 帮助
```

- stdout 永远是合法 JSON（`{"ok":true,"data":...}`），`--pretty` 缩进供人读
- `start` 已有活实例 → `conflict`；未装 zread → `not_found`
- `stop` 无活实例幂等返回 `stopped=false`，不算失败
- 跨进程实例定位：pidfile（`UserConfigDir/zreadmanager/running.json`）记录 PID + 进程创建时间，探活双校验防 PID 复用误杀

## 数据文件

| 路径 | 用途 |
|---|---|
| `UserConfigDir/zreadmanager/running.json` | 活实例记录（pid/dir/参数/创建时间） |
| `UserConfigDir/zreadmanager/config.json` | 上次工作区（last_dir） |
| `TempDir/zreadmanager.log` | zread browse 子进程输出日志 |

## 构建

```
go build ./cmd/zreadmanager
```

## 与 zread-tray 的关系

zread-tray（GUI 版）已归档至父仓 `.archived/go_projects/zread-tray`，由本项目替代；二者不共享运行状态：zreadmanager 有独立的 pidfile 与配置目录。
