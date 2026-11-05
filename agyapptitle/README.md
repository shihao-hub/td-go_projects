# agyapptitle —— Antigravity 会话标题智能汉化与后台守护进程

`agyapptitle` 是专为 Google Antigravity (Desktop App 与 CLI) 打造的本地后台守护与会话标题智能汉化工具。

基于对 Antigravity 核心后端（`language_server.exe`）底层会话标题硬编码机制（独立 Out-of-band Sidecar RPC 与硬编码英文 System Prompt）的深度逆向工程定论，通过外挂数据层对齐、极速大模型生成与官方 ConnectRPC 内存热推送，实现会话标题从生成到展示的全链路中文化。

---

## 核心特性

- 🚀 **极速响应与零配凭据**：
  - 模型链方案沿用前期已验证的设计（原 `acp-title-proxy`，该项目现已归档），优先通过 OpenCode Go 网关（`deepseek-v4.1-flash`，0 思考）实现 **1 秒极速生成**；
  - 自动读取本机 `~/.local/share/opencode/auth.json`，无需反复搬运或配置 API Key。
- ⚡ **官方 ConnectRPC 实时热推送**：
  - 动态探测正在运行的 `language_server.exe` 进程与 `--csrf_token`，扫描监听端口；
  - 实时向 `/exa.language_server_pb.LanguageServerService/UpdateConversationAnnotations` 发送更新，内存状态即时同步，彻底根绝后台后台定时协调时的“旧内存反向覆写”。
- 🔒 **四重原子双写保障**：
  - **SQLite `title` 列** 与 **`raw_summary` BLOB**：纯 Go 原生实现 Protobuf Wire 编解码，原子重打包 Field 1 与 Field 15，避免界面回退旧标题；
  - **WAL 截断**：显式执行 `PRAGMA wal_checkpoint(TRUNCATE)`，事务即时固化入主库；
  - **`agyhub_summaries_proto.pb`**：同步更新 Protobuf 字典映射；
  - **`annotations/<id>.pbtxt`**：同步写入用户级文本注解。
- 🛡️ **智能防抖与熔断机制**：
  - 严格分析汉字与英文单词占比，自动剥离 `【待办】`、`[WIP]` 等标签前缀，消灭混血伪中文；
  - 单会话失败达 3 次自动熔断，避免死循环消耗 Token 配额；
  - 本地状态记账于 `%APPDATA%\language_projects\agyapptitle\state.json`。
- 🖥️ **Windows 零黑框后台守护**：
  - 支持 `start`、`stop`、`status` 管理后台常驻进程，彻底消除命令行黑窗。

---

## 构建与通道隔离 (Dev vs Release)

严格遵循 Monorepo《各语言子仓约定》中的 dev/release 隔离规范：

| 维度 | 开发版 (Dev) | 正式版 (Release) |
|---|---|---|
| **构建命令** | `uv run scripts/build.py` | `uv run scripts/build.py --release` |
| **产物文件** | `agyapptitle-dev.exe` / `agyapptitlew-dev.exe` | `agyapptitle.exe` / `agyapptitlew.exe` |
| **版本号** | 带 `-dev` / `-dirty` 标识（如 `v1.0.0-dev`） | 纯净正式版本号（如 `v1.0.0`） |
| **数据目录** | `%APPDATA%\language_projects\agyapptitle\dev` | `%APPDATA%\language_projects\agyapptitle\prod` |
| **开机自启** | **强校验拦截**（禁止注册，防止污染系统） | **允许注册**（绑定无黑框 `agyapptitlew.exe`） |

---

## 命令速查 (CLI Reference)

```powershell
# 1. 单次扫描并汉化当前未汉化的所有会话（一键治理）
agyapptitle sync

# 2. 预览模式（不实际写入数据库）
agyapptitle sync --dry-run

# 3. 指定目标环境与输出机器 JSON
agyapptitle sync --target app --json

# 4. 前台运行监听模式（控制台实时彩色日志调试）
agyapptitle watch

# 5. 后台静默启动守护进程（零黑框）
agyapptitle start

# 6. 查看守护进程运行状态与汉化统计
agyapptitle status

# 7. 停止后台守护进程
agyapptitle stop

# 8. 一键开启 Windows 开机自启（自动绑定无黑框版本 agyapptitlew.exe）
agyapptitle autostart enable

# 9. 查看与关闭开机自启
agyapptitle autostart
agyapptitle autostart disable

# 10. 零 I/O 静态导出 JSON Schema 契约
agyapptitle schema
```

---

## 构建方式

使用 uv 或 python 运行项目内置 PEP 723 单脚本构建流水线：

```powershell
python scripts/build.py
```

产物输出在 `bin/` 目录下：
- `bin/agyapptitle.exe`：全能控制台版（支持全部命令与前台 watch）；
- `bin/agyapptitlew.exe`：静默无黑框后台版（带 `-H=windowsgui`）。
