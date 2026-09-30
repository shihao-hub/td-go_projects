# plansql — 基于 SQL 追加流与 SQLite 投影的 Plan/Spec 状态管理与约束 CLI

遵循《CLI 工具开发标准 v2》（daemon 架构）实现的 Plan 与 Spec 状态治理工具。

## 核心设计理念

1. **事件溯源（Append-Only WAL）**：
   - 物理真理层是纯文本文件 `plans_status.sql`，只增不减，支持清晰的 Git Diff 追踪与人工裁决。
   - SQLite 仅作为运行时内存投影视图（Materialized View），不入 Git。
2. **防呆与防错**：
   - AI 和人类均通过 CLI / MCP 安全追加标准 SQL，杜绝手工拼接引号、语法或转义导致的数据库崩溃。
   - 随时通过 `plansql check` 静态排错。
3. **架构规范**：
   - 业务逻辑仅编译入守护进程 `plansql serve`；
   - CLI 终端命令、Web UI 看板、MCP 桥接工具均为薄客户端。

## 安装与编译

使用 uv 运行项目构建脚本（PEP 723 内联元数据，无第三方依赖；版本与 buildID 由 `git describe` 注入）：

```powershell
cd go_projects/plansql
uv run scripts/build.py            # 开发构建：describe 含短 hash / -dirty
uv run scripts/build.py --release  # 发布构建：仅干净 plansql/vX.Y.Z tag 检出
```

手动构建（不注入构建指纹，运行时回退 Go 编译期 VCS 信息）：

```powershell
cd go_projects/plansql
go build -o plansql.exe ./cmd/plansql
```

两种方式产物均为项目根下的 `plansql.exe`。

## CLI 常用命令

```bash
# 1. 扫描当前工作区所有 **/plans/** 和 **/specs/** 文档与 SQL 的对齐情况
plansql scan

# 2. 列出所有已登记状态
plansql list
plansql list --json

# 3. 校验 plans_status.sql 文件的语法与约束完整性
plansql check

# 4. 更新/登记某篇文档的状态 (原子追加至 plans_status.sql 并更新内存库)
plansql set docs/plans/28-plansql-tracker.md --status completed
plansql set docs/plans/28-plansql-tracker.md --status '{"state":"in_progress","note":"Task 1 done"}'

# 5. 唤起浏览器查看可视化看板
plansql ui

# 6. 导出 MCP JSON Schema 契约
plansql schema
```

## MCP 集成 (给 AI Agent 使用)

在客户端配置中增加 stdio 命令：

```json
{
  "mcpServers": {
    "plansql": {
      "command": "D:/Users/language_projects/go_projects/plansql/plansql.exe",
      "args": ["mcp"]
    }
  }
}
```
AI 在执行完某个 plan 或 spec 的任务后，可直接调用 `plansql_set_status` 工具原子登记，并由 Git 独立提交。
