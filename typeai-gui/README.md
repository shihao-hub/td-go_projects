# typeai-gui

`typeai-gui` 是 [typeai](../typeai) 的桌面终端窗口壳：双击打开一个窗口，窗口内就是原汁原味的 typeai TUI。壳不重画任何图形界面，typeai.exe 零改动。

```text
typeai-gui.exe 窗口
 └─ xterm.js 渲染（VS Code 内置终端同款）
     └─ Wails 事件桥（base64 字节流，保真 UTF-8）
         └─ ConPTY（Windows 伪控制台）
             └─ typeai.exe（TUI 原样运行）
```

## 前置条件

- Windows 10 1809+（ConPTY）/ WebView2 运行时（Win11 自带）
- typeai.exe，按以下优先级定位：
  1. 环境变量 `TYPEAI_GUI_TYPEAI_PATH` 显式指定
  2. 与 typeai-gui.exe 同目录的 `typeai.exe`（便携部署，推荐把两个 exe 放一起）
  3. `PATH` 中的 `typeai.exe`

找不到时窗口会显示已尝试的位置与配置方法，不会闪退。

## 使用

```powershell
typeai-gui.exe        # 打开终端窗口，自动进入 typeai TUI
```

- 窗口缩放即时同步到 TUI（ConPTY 触发 Bubble Tea 重绘）
- 关闭窗口即终止 typeai 子进程并释放 ConPTY，无后台残留
- typeai 会话异常退出时按 `Alt+R` 可重启会话
- typeai 自身的配置、session 数据目录不变（`%APPDATA%\language_projects\typeai\`）；壳自身不产生数据文件

## 构建

```powershell
uv run build.py          # 自动判定：HEAD 命中 tag 且工作区干净 → release，否则 dev
uv run build.py --dev    # 强制 dev（build/typeai-gui-dev.exe，保留控制台）
uv run build.py --release
```

依赖：Node/npm、wails3 CLI（v3.0.0-beta.25）、MSYS2 MinGW64（windres，PE 图标资源）、
Go 1.26。图标由 `scripts/render_icon.py` 生成（`uv run scripts/render_icon.py`）。

## 开发

```powershell
go test ./...            # ConPTY 会话层测试（cmd.exe 模拟子进程）
go test -race ./internal/terminal -count=1   # 需要 MSYS2 MinGW64 gcc（CGO_ENABLED=1）
cd frontend; npm run dev # 前端热更开发（端口 9246）
```

## 仓库约定说明

- go_projects 以 CLI 工具为主，GUI 项目属例外：收录理由为 typeai 的桌面形态诉求（用户确认的"终端结构 GUI"），与 glmquotawatch-gui 同类。
- 按《CLI 工具开发标准》豁免 CLI/MCP/schema：本项目的交互面就是内嵌终端（typeai 本体已提供 `schema`/`--json` 契约），壳自身无管理命令需求，不重复提供。
