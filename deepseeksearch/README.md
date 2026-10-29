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
