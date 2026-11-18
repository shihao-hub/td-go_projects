# DeepSeek Search Studio (深搜工作台)

基于 DeepSeek 官方搜索与抓取独立接口（Search API + Fetch API）构建的双通道桌面应用（遵循 `sh-cmd-desktop-dual-channel` 规范）。

---

## 一、双通道产物设计

| 产物名称 | 通道类型 | 子系统 | 核心职责 |
|---|---|---|---|
| `bin/deepseeksearch-gui[-dev].exe` | **GUI 桌面通道** | `windowsgui` (无黑框) | 现代化桌面工作台，托盘常驻，关窗释放 WebView 内存，窗口几何记忆，内嵌 WebUI |
| `bin/deepseeksearch-cli[-dev].exe` | **Console 命令行通道** | `console` | 终端子命令、脚本与自动化管道支持（支持 `--json`、`--content`、管道重定向） |

---

## 二、覆盖的核心接口能力

### 多轮研究工作台

沿「搜索词工作台 → 多路搜索 → 根据反馈补搜」持续调查一个问题，GUI 与 CLI 共用研究记录：

- 输入原始问题；手工编辑搜索词，或按需让 AI 追加带目的说明的候选词。
- AI 候选默认不勾选；只执行选中的查询。每轮最多 6 路，默认每路 10 条、并发 3 路。
- 合并重复 URL，按排名融合排序；保留来源查询、原始排名及分值，标明本轮新增网页。
- 支持整体或单条结果反馈：太泛、偏题、缺少实际案例、缺少反面材料、自由文本。
- 反馈保存后主动生成补搜建议；未选中的草稿保留，已执行查询移入轮次历史。
- 自动保存草稿与执行进度，重新打开后恢复；部分失败可只重试失败项，取消保留已完成结果。
- AI 复用现有 Key/Base URL，设置里的「扩词与补搜模型」默认 `deepseek-flash`。

模型只负责建议搜索策略。生成建议会发送原始问题、草稿、近期查询和反馈，以及至多 12 条结果标题与片段；不会发送抓取正文或 Key。

### CLI 研究流程

```powershell
# 创建研究，记录返回的研究 ID
.\bin\deepseeksearch-cli-dev.exe research create --question "如何选择 Go 桌面框架？" --json

# 生成建议，仅保存候选，不自动搜索
.\bin\deepseeksearch-cli-dev.exe research suggest --id <研究ID> --json

# 编辑 UTF-8 JSON 草稿后保存；可从 show 输出复制 revision 防止覆盖其他入口的更新
.\bin\deepseeksearch-cli-dev.exe research set-queries --id <研究ID> --file draft.json --json
.\bin\deepseeksearch-cli-dev.exe research run --id <研究ID> --json

# 保存反馈，再生成下一轮建议
.\bin\deepseeksearch-cli-dev.exe research feedback --id <研究ID> --kind need_examples --text "需要真实工程案例" --json
.\bin\deepseeksearch-cli-dev.exe research suggest --id <研究ID> --json

# 恢复、列出、重试；Ctrl+C 停止本轮，已完成结果保留
.\bin\deepseeksearch-cli-dev.exe research show --id <研究ID> --json
.\bin\deepseeksearch-cli-dev.exe research list --json
.\bin\deepseeksearch-cli-dev.exe research run --id <研究ID> --retry-failed --json

# 离线导出新增研究命令的契约，不访问数据库或网络
.\bin\deepseeksearch-cli-dev.exe schema
```

`draft.json` 示例（文件须使用 UTF-8 无 BOM）：

```json
{
  "queries": [
    {"query": "Wails 官方文档 限制", "reason": "确认边界", "selected": true},
    {"query": "Go desktop framework production experience", "reason": "实际案例", "selected": true}
  ],
  "count": 10,
  "needContent": false,
  "localization": ""
}
```

新命令默认人读，`--json` 输出单个 `ok/data/error` 包络，进度写 stderr。退出码：成功 0、参数错误 2、其他失败 1；部分失败同时保留 `data`。反馈类型为 `too_broad`、`off_topic`、`need_examples`、`need_counterexamples`、`custom`；可加 `--result <结果ID>` 针对单条结果。`--refresh` 跳过研究缓存。

原有单次 `search`、`fetch` 等命令保持兼容。`schema` 目前仅覆盖新增 `research` 命令，不代表存量命令已经全部迁移。

GUI 使用的 HTTP 接口与 CLI 调用相同 Service，输入/结果沿用公开 DTO：

| 用例 | CLI | HTTP |
|---|---|---|
| 创建与列出研究 | `research create/list` | `POST/GET /api/research` |
| 恢复研究及查看进度 | `research show --id ID` | `GET /api/research/ID` |
| 替换当前草稿 | `research set-queries` | `PUT /api/research/ID/draft` |
| 按需追加 AI 候选 | `research suggest` | `POST /api/research/ID/suggest` |
| 执行选中查询或重试失败项 | `research run` | `POST /api/research/ID/run` |
| 保存反馈 | `research feedback` | `POST /api/research/ID/feedback` |

写接口使用 `application/json`。执行入参为 `retryFailed`、`refresh`；草稿支持可选 `revision` 乐观锁。HTTP 同样返回 `ok/data/error`，参数错误 400、记录不存在 404、修改冲突 409、部分成功 207、内部或上游失败 502。部分成功必须检查 `ok` 并保留 `data`。状态查询可在执行请求期间轮询，取消执行请求即向底层传播取消。

为限制本地存储与上下文规模，单个研究最多 100 轮、200 条反馈和 32 MB 快照；列表展示最近 100 个研究，较早的记录仍可通过 ID 查看。研究结果按 URL 规范化后融合，保留查询参数以避免错误合并不同页面；原始相关度分值仅作来源信息，不直接跨查询比较。

研究缓存有效期 24 小时，区分原始问题、查询、条数、正文需求、地域和服务地址。每路请求最多 60 秒，整轮最多 150 秒；同一研究的并发修改会被拒绝。异常退出后租约最多 180 秒到期，恢复记录时将未完成项标为中断，由用户手动重试。

首次升级自动在同一数据目录生成 `deepseeksearch-pre-research-*.db` 一致备份。恢复时关闭两个通道，在独立目录使用该备份及旧版程序核对数据；不要直接覆盖仍在运行的 SQLite 文件。

---

1. **Search API (`POST https://api.deepseek.com/v1/tools/search`)**
   - 网页搜索，支持 `site:`、`-site:` 与引号精确匹配
   - 支持设置返回条数 `count` (1~50)
   - 支持提供 `ori_question` 上下文问题优化意图重排
   - 支持 `need_content=true` 直接获取抓取正文
   - 结构化展示 `rerank_score`（相关度分值）与 `authority`（权威等级）
   - 快速在 `snippet`（导读片段）与 `summary`（LLM长摘要）之间切换
2. **Fetch API (`POST https://api.deepseek.com/v1/tools/fetch`)**
   - 网页正文去噪提取为纯净 Markdown 文本
   - 排版视图与 Markdown 原文实时切换
   - 一键复制 Markdown、一键下载 `.md` 文件
3. **本地离线知识库与历史管理**
   - 每次搜索与抓取记录自动沉淀在本地 SQLite 中，断网或离线随时回看。

---

## 三、快速开始

### 1. 配置 API Key（两种方式均可）

- **在 GUI 界面中配置**：
  双击运行 `deepseeksearch-gui-dev.exe`，点击右上角设置图标，填入 DeepSeek API Key 并点击“连通性测试”与“保存配置”。
- **在 CLI 终端中配置**：
  ```bash
  # 设置 API Key
  deepseeksearch-cli-dev.exe config set-key sk-xxxxxxxxxxxxxxxx
  
  # 查看当前配置
  deepseeksearch-cli-dev.exe config show
  
  # 测试连通性
  deepseeksearch-cli-dev.exe config test
  ```

### 2. CLI 常用命令

```bash
# 1. 网页搜索
deepseeksearch-cli-dev.exe search "DeepSeek API 搜索接口" --count 5

# 2. 结构化 JSON 输出（适合管道配合 jq 处理）
deepseeksearch-cli-dev.exe search "AI Agent 框架" --count 3 --json

# 3. 搜索并同时抓取正文
deepseeksearch-cli-dev.exe search "Go Wails 双通道桌面" --content

# 4. 抓取网页并提取为 Markdown
deepseeksearch-cli-dev.exe fetch "https://en.wikipedia.org/wiki/DeepSeek"

# 5. 抓取网页并保存为本地文件
deepseeksearch-cli-dev.exe fetch "https://example.com" --save article.md

# 6. 启动无头 WebUI 服务
deepseeksearch-cli-dev.exe serve --port 8080 --open
```

### 3. 数据与隔离规范

数据与配置文件严格保存在用户本地 AppData 目录，dev 与 prod 物理隔离：
- **开发版 (Dev)**：`%APPDATA%\language_projects\deepseeksearch\dev\`
- **发行版 (Prod)**：`%APPDATA%\language_projects\deepseeksearch\prod\`
- 包含文件：
  - `deepseeksearch.db`：SQLite 数据库（配置与检索历史）
  - `window.json`：窗口几何位置与尺寸记忆
  - `deepseeksearch.log`：全通道诊断落盘日志
