# termhub

Windows 终端会话与启动项生命周期管理器。终端即用即抛，记忆与状态留给管理器。

- Spec：父仓 `docs/projects/go_projects/termhub/specs/01-termhub-terminal-manager-spec.md`
- 看板：父仓 `docs/projects/go_projects/termhub/plans/01-termhub-vertical-slices.md`
- 工程基线：`sh-go-wails-desktop`（Wails v3 单通道 GUI 形态）

## 当前进度

- [x] T01 曳光弹：可嵌入 PowerShell（ConPTY + xterm.js + WS 事件桥）
- [x] T02 启动项与启动组：配置 CRUD、一键拉起、自动命令
- [x] T03 命令记忆：每会话命令文件 + PSReadLine 全局历史
- [x] T04 会话状态面板：状态/时长/cwd/命令/内存
- [x] T05 关闭即回收：确认弹窗 + Job Object 整树终止
- [x] T06 会话元数据落盘（内部诊断/命令数据源；UI 不再留存「已退出」条目）
- [x] T07 AI 诊断：最近命令 + 最后一屏交给本机 agy，支持追问
- [x] T08 构建流水线、图标与 dev/prod 隔离
- [x] T09 托盘常驻与关窗重建：关窗销毁 WebView、会话驻留后端、托盘重建后自动重连
- [x] T10 终端记忆模型：左侧列表=开过的终端，×=删除、自退转未启动、逐条自动重开（默认不拉起）

## 终端记忆模型（用户控制会话）

- 左侧列表是持久化的终端记忆（`terminals.json`）：用户开过的终端始终记录，重启后原样恢复。
- 列表条目两态：绿点=运行中，灰点=未启动（shell 已退出或尚未拉起）。
- 条目上的 ×=删除该终端记忆（运行中先终止 shell），无任何「已退出」留存概念。
- shell 自行退出（如输入 exit）不改列表，只把该条目转回未启动；随时点击条目重新拉起。
- 每条可勾选「自动重开」：下次启动 termhub 时静默拉起纯净 PowerShell（同目录、不执行任何命令）；
  未勾选（默认）则重启后全部保持未启动。

## 形态说明

本项目**仅提供 Wails 窗口**，不提供 CLI 子命令或浏览器入口；本地 HTTP 服务只供内嵌 WebUI
使用（Wails-only 项目，按 `sh-go-wails-desktop` 的适用范围显式记录）。

## 架构

- 本地 HTTP 服务（`internal/app`）：embed 静态前端 + REST（`/api/v1/terminals` CRUD + 启动/勾选/删除、
  启动项/组 `/api/v1/config`、`/api/v1/fs/list`）+ WS 输出桥（`/api/v1/ws/{id}`）；
  动态端口 `port=0`；`NewServer` 内对勾选自动重开的终端执行启动导流
- 桌面壳（`internal/app/gui.go`）：托盘常驻、关窗销毁窗口、托盘/双击重建、dev/prod 单实例隔离
- 窗口记忆（`internal/app/window_state.go`）：尺寸/位置/最大化落 `window.json`，外接屏拔除后自动居中
- 会话接缝（`internal/session`）：SessionManager 持有 PTY；窗口重建后前端按运行中会话自动重连
- 持久层（`internal/store`）：启动项/启动组（entries.json）、终端记忆（terminals.json）、
  会话元数据（sessions.json，仅内部诊断/命令数据源）与命令历史
- PTY 抽象（`internal/pty`）：`Term` / `Factory` 接口 + `conpty` 实现；Job Object 整树终止
- AI 诊断（`internal/session` + `agy`）：最近命令 + 最后一屏输出 → `agy -p --output-format json --mode plan --sandbox`，支持 `--conversation` 追问
- 可观测性（`internal/logx`）：`<dataDir>/termhub.log` 与 stderr 双写，2 MB 轮转，GUI 无控制台也可排障
- 数据目录：`%APPDATA%\language_projects\termhub\<channel>\`（`TERMHUB_DATA` 可覆盖）

## go-wails 基线对齐

已对齐 `sh-go-wails-desktop`：托盘常驻、关窗销毁窗口 + 托盘重建、dev/prod 单实例隔离、
窗口几何/最大化记忆、`port=0` 动态端口、数据目录通道隔离、落盘日志。

旧项目评估后不采用：双通道 `-gui` / `-cli` 产物（本项目无 CLI 需求，已按 Wails-only 声明）、
开机自启门禁（当前无自启功能）、dev 地鼠图标（dev/prod 共用项目图标）。

## 联调

```powershell
uv run scripts/build.py              # dev 构建（含前端打包）
.\bin\termhub-dev.exe
```

前端改动后单独重打：`npx esbuild frontend/main.ts --bundle --outfile=internal/web/static/app.js --minify`

## 生命周期

- 新建终端 / 启动项启动 / 启动组批量拉起：每一步都先落入左侧终端记忆，再拉起 shell；随后在记忆条目上继续管理（启动、勾选、删除）。
- 删除终端记忆（×）：运行中先终止 shell（整树），记忆与最近一次运行的历史文件随之清理；无「已退出」残留。
- shell 自行退出（如输入 exit）：左侧条目转回未启动，记忆保留，点击即可重新拉起。
- 关闭主窗口：销毁 WebView2 窗口、释放内存，termhub 进程与全部 shell 继续驻留托盘。
- 托盘左键 / 双击 / 「显示主窗口」：重建窗口，前端自动重连全部运行中 shell。
- 重启 termhub：终端记忆原样恢复为未启动；仅勾选「自动重开」的条目静默拉起纯净 PowerShell。
- 托盘「退出 termhub」：终止全部 shell 后退出进程。
- Windows 11 默认把新托盘图标收进 `^` 溢出区；点开溢出后单击/双击 termhub 图标即可重建窗口，也可拖到任务栏常显。
- 构建产物为 GUI 子系统（`-H windowsgui`，无控制台）；日志见 `<dataDir>/termhub.log`。
