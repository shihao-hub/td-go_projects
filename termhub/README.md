# termhub

Windows 终端会话与启动项生命周期管理器。终端即用即抛，记忆与状态留给管理器。

- Spec：父仓 `docs/projects/go_projects/termhub/specs/01-termhub-terminal-manager-spec.md`
- 看板：父仓 `docs/projects/go_projects/termhub/plans/01-termhub-vertical-slices.md`

## 当前进度

- [x] T01 曳光弹：点击「新建终端」拉起内嵌 PowerShell（ConPTY + xterm.js + WS 事件桥全层打通）

## 架构

- 本地 HTTP 服务（`internal/app`）：embed 静态前端 + REST（`/api/v1/sessions`）+ WS 输出桥（`/api/v1/ws/{id}`）
- 会话接缝（`internal/session`）：SessionManager，工厂注入（真 ConPTY / fake 单测）
- PTY 抽象（`internal/pty`）：`Term` / `Factory` 接口 + `UserExistsError/conpty` 实现
- 数据目录：`%APPDATA%\language_projects\termhub\<channel>\`（`TERMHUB_DATA` 可覆盖）

## 联调

```powershell
go build -o build\termhub-dev.exe .\cmd\termhub
.\build\termhub-dev.exe
```

前端改动后重新打包：`npx esbuild frontend/main.ts --bundle --outfile=internal/web/static/app.js --minify`
