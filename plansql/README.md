# plansql — 基于 SQL 追加流与 SQLite 投影的 Plan/Spec 状态管理与约束工具

遵循《CLI 工具开发标准 v2》（daemon 架构）实现的 Plan 与 Spec 状态治理工具，并提供 Wails v3 桌面工作台。

## 核心设计理念

1. **事件溯源（Append-Only WAL）**：
   - 物理真理层是纯文本文件 `plans_status.sql`，只增不减，支持清晰的 Git Diff 追踪与人工裁决。
   - SQLite 仅作为运行时内存投影视图（Materialized View），不入 Git。
2. **防呆与防错**：
   - AI 和人类均通过 CLI / MCP / WebUI 安全追加标准 SQL，杜绝手工拼接引号、语法或转义导致的数据库崩溃。
   - 随时通过 `plansql check` 静态排错。
3. **Service 核心 + 多壳**：
   - 业务逻辑只编译进 `internal/daemon` / `internal/core`；
   - CLI 终端命令、Web UI 看板、MCP 桥接、Wails 桌面壳均为薄客户端，GUI 与 CLI `serve` 共用同一份 HTTP Handler。

## 双通道产物

| 通道 | 产物 | 用途 |
|---|---|---|
| GUI | `bin/plansql-gui.exe`（dev: `bin/plansql-gui-dev.exe`） | 双击即开桌面工作台，不解析任何子命令；托盘常驻，`--hidden` 为静默托盘 |
| Console | `bin/plansql-cli.exe`（dev: `bin/plansql-cli-dev.exe`） | 终端 / 脚本 / Agent / MCP 专用，prompt、管道与重定向全合规 |

- GUI 窗口内嵌同一套 WebUI 与 REST API，**不依赖后台 daemon**；`bin/plansql-cli ui` 仍可打开由 daemon 托管的浏览器看板。
- GUI 会记住上次打开的项目根；首次启动弹出目录选择。托盘菜单与 WebUI 顶栏都支持随时切换项目。
- dev 与 release 的产物名、版本号、数据目录、单实例 ID、图标完全隔离：
  - 数据目录 `%APPDATA%\language_projects\plansql\{dev,prod}`（回退 `~/.language_projects/plansql/<通道>`）；
  - dev 使用 Go 地鼠图标，release 使用专属图标。

## 安装与编译

使用 uv 运行项目构建脚本（PEP 723 内联依赖，需要 `go-winres` 生成 PE 版本资源；Go 1.26+）：

```powershell
go install github.com/tc-hib/go-winres@latest

cd go_projects/plansql
uv run scripts/build.py            # 开发构建 → bin/plansql-gui-dev.exe + bin/plansql-cli-dev.exe
uv run scripts/build.py --release  # 发布构建 → bin/plansql-gui.exe + bin/plansql-cli.exe
```

发布构建要求 HEAD 精确命中 `plansql/vX.Y.Z` tag 且工作区干净，否则退出码 2 且不产出正式名。

手动构建（不注入构建指纹与通道，运行时回退 VCS 信息，按 dev 处理）：

```powershell
cd go_projects/plansql
go build -tags dev -o plansql-gui-dev.exe ./cmd/plansql
go build -tags cli,dev -o plansql-cli-dev.exe ./cmd/plansql
```

## CLI 常用命令

以下命令均以 Console 通道 `bin/plansql-cli.exe` 执行：

```bash
# 1. 扫描当前工作区所有 **/plans/** 和 **/specs/** 文档与 SQL 的对齐情况
plansql-cli scan

# 2. 列出所有已登记状态
plansql-cli list
plansql-cli list --json

# 3. 校验 plans_status.sql 文件的语法与约束完整性
plansql-cli check

# 4. 更新/登记某篇文档的状态 (原子追加至 plans_status.sql 并更新内存库)
plansql-cli set docs/plans/28-plansql-tracker.md --status completed
plansql-cli set docs/plans/28-plansql-tracker.md --status '{"state":"in_progress","note":"Task 1 done"}'

# 5. 启动共享 daemon 并唤起浏览器看板
plansql-cli serve --root D:/Users/language_projects
plansql-cli ui

# 6. 自省：通道、数据目录、daemon 与 GUI 运行状态、当前项目根
plansql-cli where --json

# 7. 导出 MCP JSON Schema 契约
plansql-cli schema
```

## 桌面工作台

双击 `bin/plansql-gui.exe` 即可打开；窗口尺寸、位置与最大化状态会自动记忆（`window.json`）。
托盘菜单提供：打开工作台、在默认浏览器中打开、切换项目目录、开机自启（仅 release）、彻底退出。

## MCP 集成 (给 AI Agent 使用)

在客户端配置中增加 stdio 命令（必须指向 Console 产物，GUI 产物不提供 CLI/MCP）：

```json
{
  "mcpServers": {
    "plansql": {
      "command": "D:/Users/language_projects/go_projects/plansql/plansql-cli.exe",
      "args": ["mcp"]
    }
  }
}
```

AI 在执行完某个 plan 或 spec 的任务后，可直接调用 `plansql_set_status` 工具原子登记，并由 Git 独立提交。
