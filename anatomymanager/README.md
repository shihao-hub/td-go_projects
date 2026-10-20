# Anatomy Manager (Go)

`anatomymanager` 是专为 `anatomy` 精密机械可视化 Skill 设计的产物管理器与展示系统。采用纯 Go 语言编写，内置零依赖轻量单文件 SQLite 存储引擎、结构化 CLI/JSON Schema 契约、内置 WebUI 交互式视窗，并预留 Wails 桌面端兼容接口。

---

## 🌟 核心特性

1. **SQLite 单文件全量持久化 (Single-file DB & Zero CGO)**
   - 基于纯 Go 的 `modernc.org/sqlite` 驱动，无 CGO 编译依赖，跨平台一键生成单文件二进制。
   - 所有生成的 HTML（交互网页）、MJS（绘图脚本）、PNG（预览图）、MD（原理说明）均以 BLOB 归档至数据库，脱离源工程依然能离线独立渲染。
2. **多层解耦架构**
   - **CLI 层**：全命令支持 `--json` 输出与 `schema` 格式自省，专为其他 Agent 程序化调用设计。
   - **HTTP REST 层**：轻量 API，支持图谱列表、详情查询、静态资源流式分发。
   - **WebUI 层**：纯原生 HTML + CSS + Vanilla JS（零 npm/webpack 依赖，编译期通过 `//go:embed` 打包），内置夜间/明亮主题切换、全屏 iframe 沙箱渲染、文档阅读与源码查看。
   - **Wails 兼容层**：共享前端资产与后端方法，随时可无缝打包为桌面客户端。
3. **Agent 协同接入规范**
   - 其他 Agent 在使用 `anatomy` skill 完成制图后，仅需调用一次 `anatomymanager ingest`，即可完成自动分类、元数据归纳与归档入库。

---

## 🚀 快速使用指南

### 1. 编译
```bash
cd D:\Users\language_projects\go_projects\anatomymanager
go build -o anatomymanager.exe ./cmd/anatomymanager
```

### 2. 摄取归档产物 (Ingest)
以会话 `6970a9e0-1c38-4f75-bd66-ec8a26d10e6a` 生成的精密限流定标仪为例：
```bash
anatomymanager ingest \
  --dir "D:\Users\language_projects\.agents\skills\anatomy\examples\rate-limiter" \
  --doc "C:\Users\29580\.gemini\antigravity-cli\brain\6970a9e0-1c38-4f75-bd66-ec8a26d10e6a\rate-limiter-anatomy.md" \
  --name "rate-limiter" \
  --title "台式精密限流定标仪 (Token/Leaky Bucket)" \
  --session-id "6970a9e0-1c38-4f75-bd66-ec8a26d10e6a" \
  --tags "rate-limiter,token-bucket,leaky-bucket,isometric,svg" \
  --json
```

**JSON 响应：**
```json
{
  "artworkId": "rate-limiter",
  "filesCount": 6,
  "primaryHtml": "rate-limiter.html",
  "savedAt": "2026-10-20T01:38:04.6491778+08:00",
  "status": "success",
  "title": "台式精密限流定标仪 (Token/Leaky Bucket)"
}
```

### 3. 查看图谱列表 (List)
```bash
anatomymanager list
# 或输出给 Agent 的 JSON：
anatomymanager list --json
```

### 4. 查看图谱详情 (Get)
```bash
anatomymanager get rate-limiter
anatomymanager get rate-limiter --json
```

### 5. 输出 Agent 自省 JSON Schema (Schema)
```bash
# 获取摄取接口入参格式约束：
anatomymanager schema ingest

# 获取图谱实体模型定义：
anatomymanager schema artwork
```

### 6. 还原导出 (Export)
将 SQLite 中的指定作品及其关联的全部文件还原至本地目录：
```bash
anatomymanager export rate-limiter --out ./dist/rate-limiter
```

### 7. 启动可视化 WebUI 与服务 (Serve)
```bash
anatomymanager serve --port 8765 --open
```
访问 `http://127.0.0.1:8765` 即可体验包含缩略图卡片流、深浅色渲染切换、沙箱交互、Markdown 原理解析与代码查看的全功能工作台。

---

## 📂 项目结构

```text
anatomymanager/
├── cmd/
│   └── anatomymanager/
│       └── main.go                 # 命令行入口 (CLI 子命令派发)
├── internal/
│   ├── model/
│   │   └── model.go                # 领域数据实体 (Artwork, ArtifactFile, Schemas)
│   ├── storage/
│   │   └── sqlite.go               # 纯 Go SQLite 存储引擎 (BLOB 归档 + Upsert)
│   ├── api/
│   │   └── server.go               # HTTP 路由与 REST 接口
│   └── web/
│       ├── embed.go                # //go:embed assets/*
│       └── assets/
│           ├── index.html          # WebUI 主页面
│           ├── style.css           # 纯原生精密机械风 CSS (支持 Dark/Light)
│           └── app.js              # 前端交互 (无外部依赖)
├── SPEC.md                         # Kiro Quick Spec 规范文档
├── go.mod
├── go.sum
└── README.md
```
