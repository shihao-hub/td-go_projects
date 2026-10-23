# termhub

Windows 终端会话与启动项生命周期管理器。终端即用即抛，记忆与状态留给管理器。

- Spec：父仓 `docs/projects/go_projects/termhub/specs/01-termhub-terminal-manager-spec.md`
- 看板：父仓 `docs/projects/go_projects/termhub/plans/01-termhub-vertical-slices.md`
- 工程基线：`sh-go-wails-desktop`（Wails v3 单通道 GUI 形态）

## 当前进度

- [x] T01 曳光弹：点击「新建终端」拉起内嵌 PowerShell（ConPTY + xterm.js + WS 事件桥全层打通）
- [x] T09 托盘常驻与关窗重建：关窗销毁 WebView、会话驻留后端、托盘重建后自动重连

## 形态说明

本项目**仅提供 Wails 窗口**，不提供 CLI 子命令或浏览器入口；本地 HTTP 服务只供内嵌 WebUI
使用（Wails-only 项目，按 `sh-go-wails-desktop` 的适用范围显式记录）。

## 架构

- 本地 HTTP 服务（`internal/app`）：embed 静态前端 + REST（`/api/v1/sessions`、`/api/v1/fs/list`）
  + WS 输出桥（`/api/v1/ws/{id}`）；动态端口 `port=0`
- 桌面壳（`internal/app/gui.go`）：托盘常驻、关窗销毁窗口、托盘/双击重建、dev/prod 单实例隔离
- 窗口记忆（`internal/app/window_state.go`）：尺寸/位置/最大化落 `window.json`，外接屏拔除后自动居中
- 会话接缝（`internal/session`）：SessionManager 持有 PTY；窗口重建后前端按会话快照自动重连
- PTY 抽象（`internal/pty`）：`Term` / `Factory` 接口 + `UserExistsError/conpty` 实现
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

- 关闭主窗口：销毁 WebView2 窗口、释放内存，termhub 进程与全部 PTY 会话继续驻留托盘。
- 托盘左键 / 双击 / 「显示主窗口」：重建窗口，前端自动重连所有运行中会话。
- 托盘「退出 termhub」：终止全部会话 PTY 后退出进程。
- Windows 11 默认把新托盘图标收进 `^` 溢出区；点开溢出后单击/双击 termhub 图标即可重建窗口，也可拖到任务栏常显。
- 构建产物为 GUI 子系统（`-H windowsgui`，无控制台）；日志见 `<dataDir>/termhub.log`。
