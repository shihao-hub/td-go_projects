Plan for: "/resume 会话选择弹窗与本地摘要"

**问题陈述**：当前 `/resume` 必须手输 8 位 session ID，用户无法浏览已保存会话。目标是让无参数 `/resume` 在 TUI 中弹出会话选择列表，列表展示本地生成的摘要；用户选择后恢复该会话。本期不做过滤、分页、删除、预览完整内容，也不做 AI 调用。

**需求**：
- `/resume` 不带参数时弹出会话选择列表；`/resume <session-id>` 保留手输恢复路径。
- 摘要为本地规则生成，不联网、不调用模型、不新增 session schema 字段。
- 若存在分支树，选择列表中需显示分支 ID 列表，避免 fork 会话不可见。
- 弹窗交互为最小可用：上下选择、Enter 恢复、Esc 取消。
- 会话按 `updated_at` 降序展示；当前进程所在会话不出现在列表中。
- 单个损坏 session 文件不阻断其他会话展示。
- 无已保存会话时给出明确空态提示。

**背景**：
- session 文件位于 `%APPDATA%\language_projects\typeai\sessions`，格式为 schema v3；`Session` 已包含 `id/model/created_at/updated_at/root_branch_id/active_branch_id/branches`。
- `session.LoadByID` 目前只支持按 ID 查找，没有目录枚举。
- `Chat.Resume(sessionID)` 已经能替换当前 `Store` 与内存 session；TUI `model.resume()` 负责校验运行状态、暂存图片并重建分支 UI 状态。
- TUI 已有 Bubble Tea 键位处理和 Lip Gloss 样式；新增弹窗不需要引入列表组件或新依赖。

**方案**：
1. 在 `session` 层新增 `SessionSummary` 与 `ListSessions(dataDir)`，枚举 `sessions/*.json`，解析后解析激活分支历史，并按 `updated_at` 降序返回摘要。
2. 在 `service.Chat` 暴露只读的会话摘要入口，避免 TUI 直接持有数据目录。
3. 在 TUI 增加独立会话选择状态与视图；`View()` 在选择态优先渲染弹窗，不污染主对话 viewport 状态。
4. `/resume` 无参数时打开弹窗；Enter 选中后复用现有 `model.resume(id)`；Esc 仅关闭弹窗。运行中或存在暂存图片时沿用现有错误提示，不打开弹窗。

**任务分解**：

- [x] Task 1: 提供本地会话列表与摘要
  - 文件：`internal/session/store.go`、`internal/session/store_test.go`
  - 实现：新增 `SessionSummary`（ID、文件路径、模型、更新时间、首条用户输入、最近一条 AI 回复、消息数）和 `ListSessions(dataDir)`；读取 `*.json`，跳过临时文件、损坏 JSON 与无法解析分支的文件；摘要统一压缩空白并限长。
  - 验证：`go test ./internal/session -run TestListSessions -count=1`，预期排序、字段摘要和损坏文件跳过均通过。
  - Demo：在测试数据目录写入两个不同时间更新的 session，能返回两个摘要且按最近更新在前。

- [x] Task 2: 实现 TUI 会话选择弹窗
  - 文件：`internal/tui/model.go`、`internal/tui/session_picker.go`、`internal/tui/view.go`、`internal/tui/session_picker_test.go`
  - 实现：model 增加选择态、候选列表与选中索引；`/resume` 无参数时先校验当前运行/暂存状态，再从 `Chat` 加载摘要；弹窗内拦截 Up/Down/Enter/Esc，Enter 后关闭弹窗并复用 `model.resume(id)`；视图渲染居中卡片、标题、会话摘要与底部提示。
  - 验证：`go test ./internal/tui -run TestResumePicker -count=1`，预期打开弹窗、上下移动、Enter 选中目标会话、Esc 恢复原对话视图均通过。
  - Demo：启动 TUI 输入 `/resume`，弹窗显示最近会话摘要，上下选择后 Enter 恢复。

- [x] Task 3: 更新文档并完成验收
- [x] Task 4: 会话弹窗显示分支列表
  - 文件：`internal/session/store.go`、`internal/tui/session_picker.go`、`README.md`
  - 实现：`SessionSummary` 增加分支 ID 列表；弹窗条目增加 `branch:` 行，`visibleCount` 按 4 行条目计算。
  - 验证：`python build.py -v dev` 成功；`go test ./...` 作为回归备用命令。
  - Demo：打开 `/resume`，存在 fork 的会话可见分支列表。
  - 文件：`README.md`
  - 实现：在交互命令和按键说明中补充 `/resume` 的弹窗行为、`/resume <session-id>` 兼容行为、摘要来源与边界；不改变 CLI schema。
  - 验证：`go build ./...`、`python build.py -v dev`、`.\build\typeai.exe schema`，预期 build 成功且 schema 正常输出；`go test ./...` 作为回归备用命令。
  - Demo：实际启动 `.\build\typeai.exe`，无参数 `/resume` 可见会话弹窗，选择旧会话后 Tab、输入框和状态栏全部切换为新会话。

---
**最后更新：** 2026-09-27
**作者：** AI & User
**版本：** v1.1.0
