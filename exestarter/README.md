# exestarter

exe 收藏架 CLI：目录扫描收集、集中注册、一键启动 / 定位文件 / 开 PowerShell。

从 `exe-launcher`（Win32 原生 GUI 版）复制改造而来，`model/`（Entry/Store/标签）与 `scan/`（噪音目录过滤扫描）原样继承，窗口/托盘/对话框替换为子命令 + JSON 输出（模式参考 `docs/go_projects/clictl/Go CLI JSON 输出模式参考.md`）。

## 命令

```
exestarter scan [--dir D] [--add]                          # 递归扫描 exe（--add 全部注册）
exestarter list [--status valid|invalid] [--tag T]          # 条目清单
exestarter add <path> [--name N] [--systag K] [--usertag T] # 注册单个（systag: todo|verify|broken|stable）
exestarter remove <name>                                    # 删除注册
exestarter prune                                            # 清理失效条目
exestarter run <name> [args...]                             # 前台透传启动（退出码=子进程码，未注册/失效=127）
exestarter open <name>                                      # 资源管理器定位文件
exestarter shell <name>                                     # exe 目录开新 PowerShell 窗口
exestarter help | version
```

## 行为说明

- **条目配置与 GUI 版共享** `UserConfigDir/exe-launcher/config.json`，两边互通
- 系统标签是状态语义（todo/verify/broken/stable），用户标签是自由文本，每条各至多一个
- `run` 是透传命令：stdout 只属于子进程，错误 JSON 走 stderr（与 clictl run 一致）
- `open` / `shell` 为管理命令：新窗口独立存活，CLI 立即返回 JSON
- 扫描跳过 node_modules、.git、.venv 等噪音目录，保留 dist/（打包产物常在此）
- stdout 永远是合法 JSON，`--pretty` 缩进供人读（run 的透传段除外）

## 构建

```
go build ./cmd/exestarter
```

## 与 exe-launcher / clictl 的关系

- exe-launcher 保留在仓库中（GUI 版），二者共享条目配置
- clictl 是"注册 + 启动记账"的通用 CLI；exestarter 聚焦 exe 收藏场景（扫描批量导入、状态标签、定位/开终端）
