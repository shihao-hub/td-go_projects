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

### 1. 编译（dev / release 严格隔离，每条通道两个二进制）
```bash
cd D:\Users\language_projects\go_projects\anatomymanager
uv run scripts/build.py            # 开发构建 → anatomymanager-gui-dev.exe + anatomymanager-cli-dev.exe
uv run scripts/build.py --release  # 发布构建 → anatomymanager-gui.exe + anatomymanager-cli.exe
```

- 仅当 HEAD 精确命中 `anatomymanager/v*` tag 且工作区干净时才产出 release，否则 `--release` 直接报错退出；
- 版本号由 `-ldflags` 注入：dev 为 `vX.Y.Z-dev`（脏工作区追加 `-dirty`），release 为 `vX.Y.Z`；
- dev / release 的数据目录与单实例互斥 ID 相互隔离：`%APPDATA%\language_projects\anatomymanager\{dev,prod}`；
- 开机自启仅 release 版可注册，dev 版菜单项置灰禁用。
- 图标按通道区分：dev 版用 Go 地鼠图标，release 版用专属精密机械图标（exe 文件图标与窗口/托盘图标一致）。

#### 双通道产物（为何一条通道不能只出一个 exe）

| 产物 | PE 子系统 | 用途 |
| :--- | :--- | :--- |
| `anatomymanager-gui[-dev].exe` | GUI (`-H windowsgui`) | 双击零黑框；**不承担 CLI 语义**：不管从哪里启动（双击 / 终端 / 快捷方式 / 带任意参数）都是显示桌面工作台与托盘；唯一语义参数 `--hidden`（开机自启的静默托盘，`--tray` 同义） |
| `anatomymanager-cli[-dev].exe` | Console | 终端 / 脚本 / Agent 调用：shell 正常等待、prompt 与光标正常、管道与重定向可用 |

GUI 子系统进程在 PowerShell / cmd 里是**异步启动**的（shell 打印 prompt 后立即返回），若让它兼跑 CLI，输出只能靠 `AttachConsole` 抢父控制台，必然出现「输出黏在 prompt 行 + 末尾光标悬空」；且 PowerShell 不会把管道句柄传给 GUI 进程，`--json > out.json` 会拿到空文件。这也是 git / git-gui、docker / Docker Desktop、python / pythonw 的做法：**GUI 产物不做 CLI，console 产物不做窗口**。上文示例统一用 `anatomymanager-cli`。

### 2. 摄取归档产物 (Ingest)
以会话 `6970a9e0-1c38-4f75-bd66-ec8a26d10e6a` 生成的精密限流定标仪为例：
```bash
anatomymanager-cli ingest \
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
anatomymanager-cli list
# 或输出给 Agent 的 JSON：
anatomymanager-cli list --json
```

### 4. 查看图谱详情 (Get)
```bash
anatomymanager-cli get rate-limiter
anatomymanager-cli get rate-limiter --json
```

### 5. 输出 Agent 自省 JSON Schema (Schema)
```bash
# 获取摄取接口入参格式约束：
anatomymanager-cli schema ingest

# 获取图谱实体模型定义：
anatomymanager-cli schema artwork
```

### 6. 还原导出 (Export)
将 SQLite 中的指定作品及其关联的全部文件还原至本地目录：
```bash
anatomymanager-cli export rate-limiter --out ./dist/rate-limiter
```

### 7. 启动可视化 WebUI 与服务 (Serve)
```bash
anatomymanager-cli serve --open        # 默认由系统动态探测可用端口
anatomymanager-cli serve --port 8765   # 需要固定端口时显式指定
```
启动后终端会打印实际访问地址（形如 `http://127.0.0.1:<动态端口>`），浏览器打开该地址即可体验包含缩略图卡片流、深浅色渲染切换、沙箱交互、Markdown 原理解析与代码查看的全功能工作台。

---

## 📂 项目结构

```text
anatomymanager/
├── cmd/
│   └── anatomymanager/
│       ├── cli.go                  # Console 通道入口 + 子命令 (-tags cli)
│       └── main.go                 # GUI 通道入口 (默认，只拉桌面壳)
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
