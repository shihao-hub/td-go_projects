# typeai

`typeai` 是一个本地优先的 AI 对话工具，提供两条互不干扰的使用通道：

- **桌面工作台**：双击 `typeai-gui.exe` 打开 Wails 窗口，窗口内是原生 WebUI（HTTP/SSE），支持 Markdown、thinking 折叠、分支树、会话恢复与图片输入。
- **终端 TUI**：`typeai-cli.exe` 在 TTY 下进入 Bubble Tea TUI；管理命令、脚本与 Agent 一律使用 console 通道。

两条通道共享同一套业务层、同一份配置与会话数据；GUI 与 `serve` 共用同一个 HTTP Handler，业务通信只走 HTTP/SSE，Wails 仅承担窗口、托盘与自启等宿主能力。

## 双通道产物

| 通道 | dev | release | 用途 |
|---|---|---|---|
| GUI | `typeai-gui-dev.exe` | `typeai-gui.exe` | 双击即开桌面工作台，不解析任何子命令；`--hidden` / `--tray` 为静默托盘 |
| Console | `typeai-cli-dev.exe` | `typeai-cli.exe` | 终端 TUI、`config` / `schema` / `version` / `help` / `serve` |

- GUI 产物（PE Subsystem 2）不输出任何 CLI 文本；CLI 产物（Subsystem 3）不开窗口。
- dev 与 release 的产物名、版本、数据目录、单实例 ID、图标完全隔离。

## 命令

```powershell
typeai-cli.exe                      # 在 TTY 下进入 TUI 连续对话
typeai-cli.exe serve [--port N] [--open]  # 启动 WebUI + REST/SSE 服务
typeai-cli.exe config get [--json]  # 查看有效配置（不输出密钥）
typeai-cli.exe config set --base-url U --api-key K --model M [--json]
typeai-cli.exe schema               # 导出 CLI 契约目录（零 I/O）
typeai-cli.exe version [--json]
typeai-cli.exe help
```

`serve` 与 GUI 打开的是同一个工作台：根路径返回完整 WebUI，`/api/*` 提供 REST 与 SSE。端口默认 `0`（内核动态探测），打印的地址就是实际监听地址。

## 桌面工作台

- 左侧会话列表：恢复历史会话、新建会话。
- 分支 Tab：单击切换、双击重命名、右键删除；`＋` 在最后一条 AI 回答之后创建分支。
- 消息流：Markdown 渲染、thinking 折叠、超长文本折叠、图片预览；每条 AI 消息可单独 fork。
- 输入区：Enter 发送、Shift+Enter 换行；支持选择文件、粘贴图片与拖拽上传（PNG / JPEG / WebP / GIF，单张 ≤ 10 MiB）。
- 连接设置：Base URL、模型、API Key（密钥只写不回显；环境变量优先）。
- 托盘：打开工作台、在浏览器中打开、开机自启（仅 release）、彻底退出。
- 窗口尺寸、位置与最大化状态自动记忆（`<数据目录>/window.json`）。

## 终端 TUI

对话内命令：

```text
/fork                             在当前分支最近一条完成的 AI 回答处创建子分支
/branch <id>                      切换到指定分支（如 /branch main、/branch fork-01）
/branch rm [id]                   删除指定分支（缺省删除当前分支；根分支或存在子分支时受保护）
/branch rename [old-id] <new-name> 重命名分支（支持别名 /rename <new-name>）
/resume                           打开会话选择弹窗；按最近更新时间排序并显示本地摘要
/resume <id>                      恢复指定 session ID（8 位小写十六进制）的持久化会话
/image <路径>                     添加本地图片
/detach                           清空已暂存图片
/exit, /quit                      退出
```

交互按键：

```text
Enter               发送输入；75ms 内有后续输入时转为换行
Ctrl+J              输入换行
Alt+F / Ctrl+B      在当前分支创建子分支（自动继承输入草稿或剪贴板文本，亦支持 /fork）
Ctrl+Left/Right     在分支 Tab 之间快速切换（亦支持 Ctrl+Up/Dn、Ctrl+PageUp/PgDn 或 Alt+Left/Right）
Ctrl+V              读取系统剪贴板，支持多行
Alt+V               读取剪贴板图片并插入图片标记
终端粘贴            支持 bracketed paste 多行粘贴
Ctrl+T              展开或折叠当前 thinking
Ctrl+E              展开或折叠超过 2000 字符或 40 行的 user/assistant 文本
PgUp/PgDn           翻页滚动
Up/Down             多行输入中移动光标；已到边界时逐行滚动；会话弹窗中切换选择
Ctrl+C              请求进行中先取消；空闲时退出
/exit/quit          退出
```

GLM 等思考型模型返回的 `reasoning_content` 默认折叠，只显示字符统计；可按键展开查看最近 64 KiB。思考内容只用于当前进程界面，不写入 session 历史。

图片输入：`/image <路径>` 暂存本地图片，或用 `[[image:<路径>]]` 标记；`Alt+V` 读取剪贴板图片。Windows 剪贴板图片保存到：

```text
%TEMP%\language_projects\typeai\clipboard\
```

支持 PNG、JPEG、WebP 和 GIF；单张不超过 10 MiB，每条消息最多 4 张。图片通过 OpenAI-compatible `image_url` 的 `data:` URL 发送；session 不保存 base64。

Markdown 基础渲染覆盖标题、段落、粗体/斜体、行内代码、fenced code block、有序/无序列表、引用、分隔线和链接。流式增量以低频纯文本预览显示，回答完成后一次性渲染 Markdown。

## 配置

配置文件位于当前通道的数据目录：

```text
%APPDATA%\language_projects\typeai\dev\config.json    # dev
%APPDATA%\language_projects\typeai\prod\config.json   # release
```

取不到 `APPDATA` 时回退 `~/.language_projects/typeai/<通道>/config.json`。

有效配置优先级：

```text
TYPEAI_BASE_URL / TYPEAI_API_KEY / TYPEAI_MODEL
-> config.json
-> 内置默认值
```

默认 `base_url` 是 `https://api.openai.com/v1`，默认 `model` 是 `gpt-4o-mini`。任何 OpenAI 兼容服务都可以通过 `config set` 或 WebUI 连接设置接入。`config get` 与 WebUI 只显示 `api_key_set`，不输出密钥。

## 会话持久化与分支模型

session 文件位于当前通道的 `sessions/` 目录：

```text
%APPDATA%\language_projects\typeai\<dev|prod>\sessions\YYYYMMDD-HHMMSS-<短ID>.json
```

首轮成功回答前不写文件。每轮 AI 完整回答成功、新建分支、删除/重命名分支或切换激活分支后，使用临时文件原子替换当前 session 文件。

session JSON 采用 **Schema Version 3** 结构：

```json
{
  "schema_version": 3,
  "id": "短ID",
  "model": "模型名",
  "created_at": "时间",
  "updated_at": "时间",
  "root_branch_id": "main",
  "active_branch_id": "fork-01",
  "branches": [
    {
      "id": "main",
      "parent_id": "",
      "fork_message_index": 0,
      "messages": [
        {
          "role": "user",
          "content": "问题 [[image:C:\\path\\clipboard.png]]",
          "images": [
            {
              "path": "绝对路径",
              "file_name": "图片名",
              "media_type": "image/png",
              "size": 12345
            }
          ],
          "created_at": "时间"
        },
        { "role": "assistant", "content": "回答", "created_at": "时间" }
      ],
      "created_at": "时间"
    },
    {
      "id": "fork-01",
      "parent_id": "main",
      "fork_message_index": 2,
      "messages": [],
      "created_at": "时间"
    }
  ]
}
```

- `fork_message_index` 遵循半开区间 `[0, fork_message_index)`，表示继承父分支解析后历史的前 N 条消息。
- **向后兼容**：读取 `schema_version: 2` 会话时自动映射为 `main` 根分支，后续写盘升级为 version 3。
- **旧数据迁移**：首次进入 `dev` / `prod` 通道时，会把改造前位于 `%APPDATA%\language_projects\typeai\` 根下的 `config.json` 与 `sessions/` 复制到当前通道，旧文件保留不删。

## 构建与验收

需要 Go 1.26+、`uv`、`go-winres`（PE VERSIONINFO / manifest / 图标）与 WebView2 运行时：

```powershell
go install github.com/tc-hib/go-winres@latest

cd D:\Users\language_projects\go_projects\typeai
uv run scripts/build.py            # dev     → typeai-gui-dev.exe + typeai-cli-dev.exe
uv run scripts/build.py --release  # release → typeai-gui.exe     + typeai-cli.exe
```

发布构建要求 HEAD 精确命中 `typeai/vX.Y.Z` tag 且工作区干净，否则退出码 2 且不产出正式名。

验收：

```powershell
.\typeai-cli-dev.exe version
.\typeai-cli-dev.exe schema
.\typeai-cli-dev.exe serve --open
go vet ./...
go test ./...
```

`schema`、`help`、`version` 不读取配置、不创建数据目录、不发起网络请求。

## 架构

```text
cmd/typeai/main.go      //go:build !cli   GUI 壳
cmd/typeai/cli.go       //go:build cli    TUI / 管理命令 / serve
internal/app            Wails 托盘、单实例、窗口几何、自启、日志、图标
internal/api            REST + SSE，GUI 与 serve 共用 Handler()
internal/web            内嵌 WebUI（HTML/JS/CSS）
internal/appdata        dev/prod 数据目录与旧数据迁移
internal/service        对话业务（带并发保护）
internal/tui            终端 TUI
internal/llm            OpenAI 兼容流式客户端
internal/session        会话 JSON 与分支树
internal/config         配置读写
```

数据目录：

```text
%APPDATA%\language_projects\typeai\<dev|prod>\
├── config.json
├── sessions\
├── window.json
├── typeai.log
└── webview2\
```

## 仓库约定说明

- go_projects 以 CLI 工具为主，带 GUI 项目属例外；typeai 的桌面工作台与终端 TUI 共用同一业务核心，GUI 与 `serve` 共用同一 HTTP Handler。
- CLI 契约遵循《CLI 工具开发标准》：人读输出 + `--json` + `schema`，`schema` / `help` / `version` 零业务 I/O。
- 原独立外壳 `go_projects/typeai-gui`（ConPTY + xterm.js）已归档，桌面形态由本项目 GUI 通道直接提供。
