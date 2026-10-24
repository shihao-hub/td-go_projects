# Plan for: "typeai 会话重命名与标题持久化支持"

**问题陈述**：当前 typeai 桌面 WebUI 与终端 TUI 的会话列表均只能截取首条用户输入（`first_user`）或 ID 作为显示名称，首条提问内容杂乱（如“你好”、“你是谁？”或超长命令）时无法反映会话主题；且系统没有提供会话改名能力。目标是为 typeai 增加完整的会话重命名（Title）支持，并在 WebUI 与 TUI 双通道中实现一致的呈现与交互。

**需求**：
- 在 `Session` 与 `SessionSummary` 中新增 `Title` 字段，保持与旧版本 Schema 完全向后兼容。
- 物理文件名保持 `YYYYMMDD-HHMMSS-<id>.json` 不变，仅更新文件内部 JSON 字段，防止文件路径指针断裂。
- 后端提供 `POST /api/sessions/rename` 接口，支持修改当前会话或指定历史会话的标题。
- WebUI 侧边栏：
  - 会话项优先显示自定义 `Title`，无自定义时回退至首条提问。
  - 支持双击会话项或点击悬停 ✏️ 按钮弹出修改提示框（清空则恢复默认提问预览）。
- TUI 终端：
  - `/resume` 弹窗高亮显示自定义 `Title`。
  - 支持 `/title <新标题>` 与 `/session rename <新标题>` 快速为当前会话起名。

**任务分解**：
- [ ] Task 1: 数据层与存储支持会话 Title
  - 文件：`internal/session/store.go`、`internal/session/store_test.go`
  - 实现：`Session` 与 `SessionSummary` 增补 `Title` 字段；`ListSessions` 提取 Title；新增 `UpdateSessionTitle(dataDir, sessionID, newTitle)` 原子写盘。
- [ ] Task 2: 业务层与 HTTP API 暴露重命名
  - 文件：`internal/service/chat.go`、`internal/api/server.go`
  - 实现：`Chat.RenameSession(id, title)` 业务方法；增加 `POST /api/sessions/rename` 处理函数与请求体解析。
- [ ] Task 3: WebUI 界面支持双击与悬停改名
  - 文件：`internal/web/static/app.js`、`internal/web/static/style.css`
  - 实现：会话列表渲染优先使用 `title`；为会话项绑定 `dblclick` 与悬停 ✏️ 图标交互；调用 API 后即时更新。
- [ ] Task 4: TUI 弹窗展示与改名命令
  - 文件：`internal/tui/model.go`、`internal/tui/session_picker.go`
  - 实现：`/resume` 会话弹窗优先展示自定义 Title；增加 `/title` 命令行支持。
- [ ] Task 5: 自动化测试与全量回归验证
  - 验证：运行 `go test ./...` 与 `python scripts/build.py -v dev`。
