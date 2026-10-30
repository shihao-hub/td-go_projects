# agysess —— 轻量级 agy CLI 会话管理与 Windows Terminal 联动工具

`agysess`（Antigravity Session Manager）是一款专为 Google Antigravity `agy` CLI 打造的极致轻量、双模（CLI + 原生 GUI）会话管理工具。

针对 Antigravity 官方桌面端常驻 1~1.5GB 内存过重的痛点，`agysess` 采用纯 Go 语言 + 原生轻量 GUI 框架 **Fyne** 开发，免除 Chromium/WebView2/Wails 运行时负担，具备秒级冷启动与极低资源占用。

---

## 核心特性

- 🚀 **极致轻量（~30MB 级内存）**：采用 Go 原生硬件加速 GUI（Fyne），拒绝 Electron / Chromium 内存暴涨，告别桌面端 1.5GB 负担。
- 🖥️ **CLI + GUI 双模驱动**：统一公共 Service 核心，既可在终端极速操作，也可启动窗口面板交互。
- 🗂️ **清晰的会话管理**：
  - 自动读取本地 `agy` 会话历史数据库；
  - 支持按关键词搜索、项目筛选、多维排序；
  - 支持会话归档/解档、自定义标题覆盖与独立备注。
- ⚡ **Windows Terminal (WT) 深度联动**：
  - **一键恢复**：在 WT 标签页中无缝恢复打开历史会话；
  - **一键新建**：在 WT 中一键拉起新会话（支持自定义工作目录、项目、模型等）；
  - **运行态实时感知**：智能探测系统中活跃运行的 `agy.exe` 进程，高亮显示 `[RUNNING: PID]`；
  - **一键关窗回收**：在面板中一键关闭指定会话的终端窗口并回收进程树，快速释放系统资源。
- 📋 **标准契约完备**：严格遵循父仓《CLI 工具开发标准》v1.0，支持人读表格、`--json` 标准包络与 `--schema` 零 I/O 离线反射。

---

## 快速上手

### 1. 命令行使用 (CLI)

```powershell
# 1. 查询会话列表（默认人读表格，隐藏已归档）
agysess list

# 2. 关键词过滤与项目筛选
agysess list -q "重构" -p "default-cli-project"

# 3. 输出标准机器 JSON 包络（供 Agent 或脚本调用）
agysess list --limit 5 --json

# 4. 查看单个会话详细信息
agysess show <session-id>

# 5. 归档 / 取消归档会话
agysess archive <session-id>
agysess unarchive <session-id>

# 6. 设置自定义标题与备注
agysess rename <session-id> "新项目调研核心会话"
agysess note <session-id> "待补充评审结论"

# 7. 在 Windows Terminal 中打开会话
agysess open <session-id>
agysess open <session-id> --new-window # 独立窗口打开

# 8. 在 Windows Terminal 中新建会话
agysess new -d "D:\workspace" -m "gemini-2.5-pro"

# 9. 探测当前运行中的 agy 会话进程
agysess running

# 10. 关闭指定运行中会话的终端窗口
agysess kill <session-id>

# 11. 离线零 I/O 导出 JSON Schema 契约
agysess schema
```

### 2. 原生桌面面板 (GUI)

直接运行静默桌面版或通过命令行唤起：
```powershell
# 启动桌面面板
agysess gui

# 或双击运行 bin/agysess-gui.exe（编译已带 -H=windowsgui，双击无黑框弹窗）
```

---

## 架构与数据存储

- **代码分层**：
  - `cmd/agysess/`：入口引导，双模分发；
  - `internal/model/`：领域模型、JSON Schema 契约；
  - `internal/store/`：
    - `agystore.go`：只读安全连接 `~/.gemini/antigravity-cli/conversation_summaries.db`；
    - `metastore.go`：管理用户自有元数据；
  - `internal/terminal/`：Windows Terminal 与 `agy.exe` 调度、进程快照探测与进程树回收；
  - `internal/service/`：公共用例业务核心（数据聚合、排序过滤、状态同步）；
  - `internal/cli/`：Cobra CLI 命令行外壳；
  - `internal/gui/`：Fyne 原生轻量桌面面板。
- **自有数据存储路径**：
  严格遵循仓库约束，存储在 `%APPDATA%\language_projects\agysess\sessions_meta.json`。

---

## 构建方式

```powershell
# 使用 Python PEP 723 脚本自动构建 CLI 与无黑框 GUI 版本
python scripts/build.py
```
构建产物输出至 `bin/` 目录：
- `bin/agysess.exe`：全功能控制台可执行文件（CLI 命令 + GUI）；
- `bin/agysess-gui.exe`：纯净桌面窗口可执行文件（双击无控制台闪烁）。
