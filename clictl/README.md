# clictl

Windows 工具注册器/启动台：注册任意 exe，`clictl run <名>` 前台透传启动，`clictl start <名>` 后台分离启动，启动过程被接管并记录。提供**双通道产物**——`clictl-gui`（Wails v3 托盘 + 内嵌 WebUI）与 `clictl-cli`（终端/脚本/Agent），二者共用同一套 HTTP 层与 SQLite 库。

## 特性

- **桌面启动台（clictl-gui）**：托盘常驻，窗口内完成注册/搜索/启动/停止/定位/编辑 meta/查看启动记录；关闭窗口不退进程，再次打开精确恢复上次尺寸与位置
- **双通道产物**：`clictl-gui[-dev].exe` 只出窗口（带任何参数都不做 CLI），`clictl-cli[-dev].exe` 是 console 子系统，prompt/管道/重定向全部合规；二者不可合并（原因见 `docs/projects/go_projects/clictl/plans/01-wails-dual-channel.md`）
- **同一 HTTP Handler**：GUI 与 `clictl serve` 托管完全相同的 WebUI + REST；WebUI 根路径永远是完整工作台，不存在「GUI 有页面、CLI 只有裸 API」的分裂
- **管理命令永远输出 JSON**：`{"ok":true,"data":...}` / `{"ok":false,"error":{"code":"...","message":"..."}}`
- **MCP server（`clictl mcp`）**：标准 MCP stdio 协议通道，把注册/启动/执行暴露为带 JSON Schema 的 MCP 工具（`clictl.list` / `clictl.run` 等 10 个）；`clictl schema` 可离线导出与 `tools/list` 同源的工具定义
- **前台透传启动（run）**：stdin/stdout/stderr/退出码全部直通子进程，不经任何 shell 包裹，管理数据不混入 stdout
- **双模式启动（start）**：窗口程序默认 DETACHED 后台分离（点火即走、不闪黑框）；`--console` 为命令行工具开独立控制台窗口（可见、可交互），记录 PID 供探活；`stop` 全量树杀
- **类型自识别**：读取 exe 的 PE 子系统，WebUI 自动区分「命令行」与「窗口程序」，主按钮对应「终端启动」/「启动」
- **真实控制台句柄**：终端启动用原生 `CreateProcess`（不设置 `STARTF_USESTDHANDLES`），窗口内 stdin/stdout/stderr 是真正的控制台设备（`isTTY=true`），TUI 程序可正常渲染与交互
- **PID 探活**：OpenProcess 存在性 + 终止态 + exe 路径比对三重校验（防 PID 回收复用误判）
- **智能提示**：`run`/`start`/`stop` 未注册名时附相似名建议；PowerShell Tab 补全子命令与工具名
- **启动记账**：启动次数、上次启动时间、耗时、退出码、后台实例 PID
- **SQLite 存储 + 通道隔离**：`%APPDATA%\language_projects\clictl\{dev,prod}\clictl.db`（取不到回退 `~/.language_projects/clictl/<通道>/`），WAL 模式，多进程并发安全

## 通道与产物

| 产物 | 子系统 | 用途 |
|---|---|---|
| `clictl-gui.exe` / `clictl-gui-dev.exe` | Windows GUI | 双击零黑框；只开桌面启动台，忽略除 `--hidden`/`--tray` 外的参数 |
| `clictl-cli.exe` / `clictl-cli-dev.exe` | Console | 终端、脚本、Agent；全部子命令只在这里 |

- dev 与 release 在**产物名、版本号、数据目录、单实例 ID、图标**五个维度完全隔离，可同时运行互不唤醒。
- GUI 通道适配：`--hidden` / `--tray` 启动静默托盘（开机自启使用）；CLI 通道下它们退化为无窗口 `serve`。

## run vs start

> **要在当前终端看输出、等它干完的用 `run`；点火就走的用 `start`；命令行工具要在独立窗口里跑的用 `start --console`。** WebUI 只提供 `start`（浏览器无法接管 TTY），需要前台透传/管道仍请用 `clictl-cli run`。

| | `run` | `start` | `start --console` |
|---|---|---|---|
| 语义 | 前台透传，阻塞到子进程退出 | 后台分离（DETACHED），立即返回 | 开独立控制台窗口，立即返回 |
| IO | 当前终端 stdin/stdout/stderr 直通 | 全部断开 | 子进程独占新窗口的控制台 |
| 退出码 | 透传子进程码 | 0 成功 / 127 未注册或失效 | 0 成功 / 127 未注册或失效 |
| 记账 | 闭环 | 只落 started_at + pid；`stop` 闭环 | 同 start；GUI/serve 常驻时窗口关闭即闭环 |

## 命令（`clictl-cli`）

| 命令 | 说明 | 退出码 |
|---|---|---|
| `clictl add <path> [--name N] [--desc D] [--meta JSON]` | 注册 exe；name 默认=文件名去 `.exe` 小写化 | 0 / 1 |
| `clictl rm <name>` | 删除注册（级联删其 launches） | 0 / 1(not_found) |
| `clictl set <name> --meta JSON` | 整体替换 meta（传 `{}` 清空） | 0 / 1 |
| `clictl list [--status active\|invalid] [--running]` | 全部工具，launch_count 降序；`--running` 只看后台活实例 | 0 |
| `clictl info <name>` | 详情 + 最近 10 条启动 + 累计耗时 + 后台运行状态 | 0 / 1 |
| `clictl run <name> [args...]` | 前台透传启动；未注册时 stderr 错误附相似名 `suggestions` | =子进程码 / 127 |
| `clictl start [--console] <name> [args...]` | 启动并输出 pid；`--console` 开独立控制台窗口，缺省 DETACHED 后台分离；已有活实例时附 `already_running` | 0 / 127 |
| `clictl stop <name>` | 全量终止该工具后台活实例（taskkill /T /F 树杀）并闭环记录 | 0 / 1 |
| `clictl cp <name> <dest_dir> [--force]` | 复制已注册 exe 到目标目录（目录须已存在） | 0 / 1 |
| `clictl serve [--port 0] [--open]` | 启动内置 WebUI + REST（与 GUI 同一 Handler；默认动态端口） | 阻塞 |
| `clictl where [--db PATH]` | 自省：通道 / 版本 / 库路径 / 工具数 / 两通道 GUI 运行状态 | 0 |
| `clictl completion powershell [--install\|--uninstall]` | PowerShell 补全脚本 | 0 / 1 |
| `clictl completion names` | 全部工具名，每行一个（raw 输出） | 0 |
| `clictl mcp` | 启动 stdio MCP server（不解析参数，日志走 stderr） | 0 / 1 |
| `clictl schema` | 导出 MCP 工具定义（与 tools/list 同源） | 0 / 1 |
| `clictl --version` | 版本号 + 通道（JSON） | 0 |
| `clictl help` | 帮助（也是 JSON） | 0 |

- 全局 `--pretty` / `--ascii`：缩进输出 / 非 ASCII 转义为 `\uXXXX`（PS 5.1 管道编码不可靠环境用），`run`/`start` 透传段除外
- 管理命令错误 JSON 走 stdout；`run` 前置错误走 stderr（stdout 只属于子进程）
- `completion powershell|names` 输出 raw 文本（消费者是 shell 补全脚本，输出即协议）

## WebUI REST（GUI 与 serve 共用）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/meta` | 通道 / 版本 / 库路径 / 工具数 / 运行中数 |
| GET | `/api/tools?status=&running=1` | 列表 / 仅运行中 |
| POST | `/api/tools` | 注册 `{path,name,desc,meta}` |
| GET | `/api/tools/{name}` | 详情 + 最近启动 + running |
| DELETE | `/api/tools/{name}` | 删除注册 |
| PUT | `/api/tools/{name}/meta` | 替换 meta `{meta:{...}}` |
| POST | `/api/tools/{name}/start` | 启动 `{args:[...],mode:"console"\|"detached"}`；mode 省略按 PE 子系统自动选择 |
| POST | `/api/tools/{name}/stop` | 树杀全部后台实例 |
| POST | `/api/tools/{name}/reveal` | 资源管理器定位 |
| GET | `/api/fs?dir=` | 只读目录浏览（注册时选 exe） |

包络与 CLI 一致（`ok/data` 或 `ok/error`），业务错误同时映射 HTTP 状态码。

## meta 白名单

`--meta` 仅允许两个 key，其余一律拒绝（`meta_unknown_key`）：

```json
{"source": "cargo", "tags": ["dev", "cli"]}
```

- `source`：string，≤ 64 字节，来源标记
- `tags`：[]string，≤ 8 项，每项 ≤ 32 字节，去重后小写存储

## 智能提示与补全

```powershell
clictl-cli run zedub                  # 错误 JSON 附 suggestions:["zedhub"]
clictl-cli completion powershell --install   # 写入 $PROFILE 标记区块，幂等
```

- 补全器只绑定 `clictl` 命令名，内部 try/catch 静默失败
- `--install` 安装块自带两行 UTF-8 编码固化（修 PS 5.1 原生 exe 间管道中文变 `?`）；旧 3 行块重跑自动升级为 5 行（`upgraded:true`）

## 构建

```powershell
cd go_projects/clictl
uv run scripts/build.py             # dev → clictl-gui-dev.exe + clictl-cli-dev.exe
uv run scripts/build.py --release   # release → clictl-gui.exe + clictl-cli.exe（需 tag + 干净工作区）
```

- release 门禁：HEAD 精确命中 `clictl/v*` tag，且 `go_projects` 工作区无未提交改动
- PE 资源由 `go-winres` 生成（VERSIONINFO + manifest + 图标）；GUI 与 CLI 的 FileDescription 不同
- dev 使用 Go 地鼠图标，release 由 `scripts/render_icon.py` 重绘专属图标
- 依赖 `modernc.org/sqlite`（纯 Go 无 CGO）、`golang.org/x/sys`（Win32 探活）、`github.com/wailsapp/wails/v3`（仅 GUI 产物）
- 安装到用户工具目录（CLI 安装为 `clictl.exe`，GUI 同通道安装为 `clictl-gui.exe`）：`uv run scripts/copy_clictl_to_local_bin.py [--channel prod]`；卸载用 `remove_clictl_from_local_bin.py`

## 数据目录

- `%APPDATA%\language_projects\clictl\dev\clictl.db`（dev 产物）
- `%APPDATA%\language_projects\clictl\prod\clictl.db`（release 产物）
- 双通道改造前的旧库 `%APPDATA%\language_projects\clictl\clictl.db` 会在**每个通道首次启动时各复制一份**（含 `-wal`/`-shm`，旧文件保留），此后两通道数据独立演进
- 窗口几何记忆：`<通道目录>\window.json`；诊断日志：`<通道目录>\clictl.log`

完整设计见 `PLAN.md` 与父仓 `docs/projects/go_projects/clictl/plans/01-wails-dual-channel.md`；使用文档见父仓 `docs/projects/go_projects/clictl/clictl 使用指南.md`；表结构变更存档见父仓 `docs/projects/go_projects/clictl/migrations/`；其他 CLI 项目复用 JSON 输出模式见 `Go CLI JSON 输出模式参考.md`。
