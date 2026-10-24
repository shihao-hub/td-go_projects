# 方案实施计划：TypeAI 界面细化 v2（动态标签色板、搜索与去噪）

> 计划编号：04（父仓编号：59）  
> 关联设计：`.workbuddy/outputs/typeai-ui-redesign-v2.html`（像素君）  
> 目标：顺延「动态标签 + 树状分组 + 偏好持久化」，落地 8 色稳定色板、消除会话徽标冗余、相对时间阶梯、全局 Ctrl+K 搜索、Composer 光晕与空状态引导等细化点。

---

## 需求与设计评估

对比当前实现（已完成去 AI 味与偏好持久化），v2 设计方案具有极高的工程可行性与体验价值：
1. **消除信息冗余**：分组内已明确表达的标签不再在卡片内重复展示，大幅降低侧栏视觉负担；
2. **8 色低饱和稳定色板（Hash 循环取色）**：以待办（琥珀）为基准，其余动态标签按字符串 hash 取 1~7 号色，明度均一，四端同源同色（组头色点、卡片徽标、弹窗 Pill、全库标签池）；
3. **相对时间阶梯**：从冗长的秒级时间戳转换为「今天 HH:mm / 昨天 / 周几 / MM-DD」，hover 时保留完整时间；
4. **Ctrl+K 搜索检索**：侧边栏支持即时过滤（标题、标签、ID），配合快捷键与 Escape 清空；
5. **质感打磨**：WCAG AA 对比度提亮、Composer 聚焦柔和光晕、活动卡片左缘 3px 琥珀色指示条、空状态品牌引导。

---

## 任务分解

- [ ] Task 1: 样式体系升级与 CSS 变量色板配置
  - 文件：`internal/web/static/style.css`、`internal/web/static/index.html`
  - 实现：
    - 注入 8 色动态标签色板（`--tag-0` ~ `--tag-7`）与提亮后的语义对比度 Token（`--text`、`--muted`、`--faint`、`--panel-3`、`--accent-line`）；
    - 实现活动卡片左指示条（`.session-item.active::before`）与呼吸动画（`@keyframes pulse`）；
    - 实现 Composer 聚焦光晕、实底琥珀发送按钮、标签 `.badge` 与 `.tag-pill` / `.pool-chip` 的色点 `.b-dot` 与 `color-mix` 样式；
    - 在 HTML 侧边栏结构中加入搜索框容器（`.search-wrap`、`#session-search`、`<kbd>Ctrl K</kbd>`）。
  - 验证：CSS 语法规范，无未定义变量引用；页面骨架符合设计稿。

- [ ] Task 2: 标签稳定 Hash 取色与卡片徽标去冗余逻辑
  - 文件：`internal/web/static/app.js`
  - 实现：
    - 实现 `getTagColor(tag)`：待办固定 `--tag-0`，归档固定 `--faint`，其它动态标签通过稳定哈希循环映射至 `--tag-1` ~ `--tag-7`；
    - 修改 `renderSessionCard(item, currentSessionID, currentGroupTag)`：当卡片在特定标签组（如“待办”、“归档”、或动态标签“python”）内展示时，自动过滤隐藏与组头同名的标签，消除视觉重复；平铺模式下完整呈现；
    - 在树状组头添加对应色点（`<span class="tree-dot">`）；
    - 升级会话属性弹窗（`openSessionModal`）：标题展示短 ID，选中的 Pill 与标签池 Chip 注入 `--tag-c` 动态色与色点。
  - 验证：`node -c internal/web/static/app.js` 语法无误；在不同分组下徽标不再重复。

- [ ] Task 3: 相对时间阶梯与 Ctrl+K 侧栏会话搜索
  - 文件：`internal/web/static/app.js`
  - 实现：
    - 实现 `formatRelativeTime(dateStr)`，按“刚刚 / 今天 HH:mm / 昨天 / 周几 / MM-DD / YYYY-MM-DD”格式化，卡片 meta 保留原生完整时间作为 `title`；
    - 绑定搜索输入框与全局快捷键（`Ctrl+K` / `Cmd+K` 聚焦，`Escape` 清空并失焦）；
    - 在 `renderSessions()` 中加入基于 `state.searchQuery` 的即时过滤，匹配标题、标签、首句提问；搜索态下动态展示匹配的分组与条目。
  - 验证：快捷键响应灵敏，搜索输入实时过滤列表。

- [ ] Task 4: 对话流空状态与 Tab 运行态指示升级
  - 文件：`internal/web/static/app.js`、`internal/web/static/style.css`
  - 实现：
    - 升级 `renderTranscript()` 空状态为设计稿的轻量品牌方块（logo `t`）+ 快捷键使用说明；
    - 运行态 Tab 使用呼吸脉冲圆点代替纯文本 `*`。
  - 验证：无消息时空状态显示正常，快捷键点击可交互。

- [ ] Task 5: 综合回归与双通道产物构建
  - 文件：`internal/web/static/*`
  - 实现：
    - 执行 `go test -v ./...` 确保所有后端与集成测试通过；
    - 运行 `uv run scripts/build.py` 确保 GUI 与 CLI 构建正常。
  - 验证：双通道产物编译成功，测试全绿。

---
**版本：** v1.0.0  
**状态：** 规划中 (In Progress)
