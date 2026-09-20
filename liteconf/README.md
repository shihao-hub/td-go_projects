# liteconf

轻量配置中心：`.env` 的替代品、极简版 Apollo。单实例 Go server 托管各应用各环境的 JSON 配置文件，配套 Go client SDK 实现配置热更新与变更回调。server 与 client SDK 零第三方依赖，仅 Go 标准库；调试 CLI 因 MCP 入口引入官方 [go-sdk](https://github.com/modelcontextprotocol/go-sdk) v1.8.0。

定位**内网/本机可信环境**：HTTP 明文、无鉴权，请勿暴露公网。

## 特性

- **文件即数据库**：配置按 `configs/{app}/{env}.json` 二级目录存储，直接用编辑器改文件就能生效
- **版本化**：每次变更版本号 +1，内存维护版本/哈希/修改时间元数据
- **长轮询热更新**：客户端携带已知版本挂起，变更秒级感知（Apollo 同款机制）
- **外部编辑检测**：绕过 API 直接改文件，server 周期扫描（默认 5s）自动感知并广播
- **原子写入**：临时文件 + rename，失败不留半截配置
- **SDK 容错**：断线指数退避自动重连，故障期间继续读最后一份可用配置
- **内嵌 Web 控制台**：浏览器访问 `/ui/` 即可完成配置浏览、查看、双模式编辑（JSON 文本 / 键值表）与新建，静态资源经 `go:embed` 随二进制分发
- **MCP 入口**：`liteconf.exe mcp` 启动 stdio MCP server（官方 go-sdk v1.8.0，协议基线 2025-11-25），AI 客户端可直接发现、读取、写入配置
- **调试 CLI**：`http` 子命令把参数原样透传给 curlie（HTTPie 语法 + curl 引擎）调试 API，`schema` 导出契约目录（MCP 工具 + CLI 命令同源）
- **零依赖数据面**：server 与 client SDK 仅用标准库，`go build` 即得单文件二进制（server 二进制不含 MCP 代码）

## 快速开始

### 构建与启动

```powershell
cd go_projects\liteconf
.\scripts\build.ps1              # 产物 build/liteconf-server.exe + build/liteconf.exe
.\build\liteconf-server.exe      # 默认监听 :8646
```

启动后浏览器打开 <http://localhost:8646/ui/> 即为 Web 控制台（`/ui` 会自动 301 到 `/ui/`）。

启动参数：

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-addr` | `:8646` | HTTP 监听地址 |
| `-root` | `%APPDATA%\language_projects\liteconf\configs` | 配置根目录（取不到 APPDATA 回退 `~/.language_projects/liteconf/configs`） |
| `-poll` | `5s` | 外部编辑检测间隔 |

日志同时写 stderr 与 `%APPDATA%\language_projects\liteconf\logs\liteconf-server.log`。

### 调试 CLI（liteconf.exe）

构建脚本同时产出 `build\liteconf.exe`（server 二进制不受影响）。`http` 子命令前提：安装 curlie（HTTPie 语法 + curl 引擎）：

```powershell
go install github.com/rs/curlie@latest
```

```powershell
.\build\liteconf.exe http GET :8646/api/app1/dev              # 读配置
.\build\liteconf.exe http :8646/api/discovery                 # 发现列表
.\build\liteconf.exe http PUT :8646/api/app1/dev key=value    # 写配置（HTTPie 键值语法）
.\build\liteconf.exe mcp                                      # 启动 stdio MCP server（默认连 http://127.0.0.1:8646）
.\build\liteconf.exe mcp -server http://192.168.1.10:8646     # 连接其他 server 实例
.\build\liteconf.exe schema                                   # 输出契约目录（JSON）：tools + commands
```

`http` 把全部参数原样透传给 PATH 中的 curlie，stdin/stdout/stderr 直通、不经 shell 包裹，`--help` 等参数归 curlie 而非 liteconf。退出码：0 成功；1 运行失败（如 curlie 缺失，stderr 给出安装指引）；2 调用参数错误；`http` 子命令透传 curlie 的退出码。版本号由构建脚本 `-Version` 参数注入（`liteconf --version` 查看）。

### MCP 接入

`mcp` 子命令是常驻 stdio MCP server：stdin/stdout 专用于协议，启动诊断写 stderr。AI 客户端（opencode、Claude Desktop 等）在 `mcpServers` 中以子进程方式接入：

```json
{
  "mcpServers": {
    "liteconf": {
      "command": "D:\\path\\to\\liteconf.exe",
      "args": ["mcp"],
      "env": { }
    }
  }
}
```

`-server` 指向运行中的 liteconf server；server 未启动时工具调用返回 `server_unreachable`。MCP 数据面与 Web 控制台、curlie 同一地位（走 `/api/*`），client SDK（`client/`）供 Go 业务进程 import，三者互不替代。

## MCP 工具

| 工具 | 对应 API | 输入 | 结构化输出 | 行为标注 |
|---|---|---|---|---|
| `liteconf.discovery` | `GET /api/discovery` | 无 | `{"ok":true,"data":{"apps":[{app,envs:[{env,version}]}]}}` | 只读 |
| `liteconf.config.get` | `GET /api/{app}/{env}` | `app`,`env` 必填；`path` 可选点路径（如 `db.host`，未命中 `path_not_found`） | `{"ok":true,"data":{app,env,version,content}}` | 只读 |
| `liteconf.config.put` | `PUT /api/{app}/{env}` | `app`,`env`,`content`（完整 JSON 对象，整体覆盖写） | `{"ok":true,"data":{app,env,version}}` | 破坏性、非幂等（每次版本 +1） |

- 业务失败返回 `isError=true` 且结构化错误 `{"ok":false,"error":{"code","message"}}`；错误码透传 server 包络（`not_found` / `invalid_name` / `invalid_json` / `internal`），客户端侧新增 `server_unreachable`、`path_not_found`。
- `liteconf schema` 输出的 `tools` 与 MCP 实际注册同源（in-memory transport 读注册视图，全分页遍历）；`commands` 为 CLI 命令契约。
- **不暴露 watch 长轮询**：挂起 30~120s 与 MCP 请求-响应的等待预算/取消语义冲突；等价替代是重读 `liteconf.config.get` 对比 `version`。

## MCP 契约变更记录

- `liteconf schema` 自本版起输出 `{"name","version","tools":[...],"commands":[...]}`，**移除存量 `interface:"cli"` 字段**（提供 MCP 入口后不再适用《CLI 工具开发标准》5.5 的 cli 例外）；tools 与 MCP 注册同源，新增 `mcp` 子命令。
- go.mod 的 go 指令自 1.22 升至 **1.25.0**（go-sdk v1.8.0 硬要求）；liteconf-server 与 client SDK 代码零改动、仍仅标准库。

### curlie 常用语法速查

curlie 兼容 HTTPie 语法；本机 Windows 自带 curl 未内置手册（`curlie --help` 不可用），下表写法均在 curlie 1.8.2 + PowerShell 5.1 实测通过，完整文档见 <https://curlie.io>。

| 写法 | 含义 |
|---|---|
| `GET url` | 请求方法，建议总是显式写——curlie 1.8.2 省略方法时本 server 会返回 `Method Not Allowed` |
| `:8646/api/app1/dev` | URL 简写，等价 `localhost:8646/...` |
| `key=value` | JSON 字符串字段，默认 `Content-Type: application/json` |
| `key:=30`、`key:=true`、`key:=[1,2]` | 原始 JSON 值（数字/布尔/数组不会被转成字符串） |
| `key:='{\"a\":1}'` | 嵌套 JSON 对象：PowerShell 5.1 下内层引号必须写成 `\"`，否则引号被参数传递吃掉、字段变成 null |
| `key==value` | URL 查询参数（拼成 `?key=value`） |
| `Header:value` | 请求头 |
| `-v` | 显示请求与响应明细 |

踩坑提示：

- HTTPie 的 `--body` / `--headers` / `--print` 在 curlie 1.8.2 不可用——会原样透传给 curl 并报 `option is unknown`；输出控制用 `-v` 或 curl 原生参数
- curlie 没有 `help` 子命令，`curlie help` 会把 `help` 当主机名去解析（`Could not resolve host`）

示例（PowerShell）：

```powershell
.\build\liteconf.exe http GET :8646/api/discovery
.\build\liteconf.exe http PUT :8646/api/app1/dev timeout:=30 db:='{\"host\":\"127.0.0.1\"}'
```

### 写入配置

```powershell
curl.exe -X PUT http://localhost:8646/api/app1/dev -H "Content-Type: application/json" --data-binary "@config.json"
# 或直接编辑 %APPDATA%\language_projects\liteconf\configs\app1\dev.json（6 秒内被感知）
```

### 接口测试

Bruno collection 位于父仓库 `docs/projects/go_projects/liteconf/brunos/`（OpenCollection YAML 格式），选 `local` 环境按编号顺序执行。

## HTTP API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/{app}/{env}` | 读取配置，返回 `{"code":"ok","data":{"content":{...},"version":N}}` |
| PUT | `/api/{app}/{env}` | 写入配置（body 即 JSON 对象），返回 `{"code":"ok","data":{"version":新版本}}` |
| GET | `/api/discovery` | 列出全部应用/环境/版本 |
| GET | `/api/watch/{app}/{env}?version=N[&timeout=S]` | 长轮询；版本已更新立即返回，相等则挂起（默认 30s，上限 120s），变更即唤醒 |

统一响应包络 `{"code":"...","message":"...","data":...}`；错误码：`not_found`（404）、`invalid_name`（400，app/env 需满足 `[a-zA-Z0-9_-]+`）、`invalid_json`（400，body 非法 JSON 或非顶层对象）。

### Web 控制台

`GET /ui/` 托管内嵌的静态控制台页面（HTML/JS/CSS 经 `go:embed` 编译进二进制，无外部文件依赖，响应均带 `Cache-Control: no-cache`）。控制台只做渲染与交互，全部业务操作（列表/读取/写入/新建）通过上述 `/api/*` 接口完成。

## 新模块接入指南

业务项目（本仓 Go 项目）三步接入：

### 1. 添加依赖

```powershell
cd <你的项目目录>
go get github.com/shihao-hub/liteconf
```

### 2. 初始化（进程启动时一次）

```go
import "github.com/shihao-hub/liteconf/client"

c, err := client.New(client.Config{
    ServerURL: "http://localhost:8646",
    App:       "taskmon", // 应用名，对应 configs/taskmon/
    Env:       "dev",     // 环境名，对应 configs/taskmon/dev.json
})
if err != nil {
    // server 不可达或配置不存在：报错退出，不会拿到空配置伪装成功
    return err
}
defer c.Close() // 停止后台长轮询，幂等可重复调用
```

### 3. 读取与订阅

```go
// 点路径读取：逐级下钻，第二个返回值为存在性标识（非静默零值）
raw, ok := c.Get("db.host") // "127.0.0.1"；键不存在 ok=false
_, ok = c.Get("")           // 空 path 返回整份配置

// 完整快照（浅拷贝，可安全持有）
cfg := c.Snapshot()

// 反序列化到业务 struct（path 为空则解码整份配置）
var dbConf struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}
err = c.Unmarshal("db", &dbConf)

// 变更订阅：配置版本变化时异步回调（旧配置, 新配置）
c.OnChange(func(old, new map[string]any) {
    // 单回调 panic/阻塞不影响其他回调与轮询循环；不保证回调间顺序
    // 典型用法：重建数据库连接池、刷新限流阈值
})

// 当前版本号（一般不直接用）
v := c.Version()
```

读取全部走本地缓存（纯内存、无锁），**业务热路径零网络开销**；网络断开期间继续返回最后可用配置，server 恢复后自动追平最新版本。

### 非 Go 语言最小接入（实战案例）

已有实战：`python_projects/sql-pg-sqlalchemy`（SQLAlchemy 学习脚手架）。**参考价值有限但姿势可抄**：只覆盖"进程启动时读一次配置"的最小场景，没有热更新、长轮询、断线兜底（那些是 Go client SDK 的能力），这种用法下配置中心 ≈ 远程 `.env`。要点：

- 一次 `GET /api/{app}/{env}`，语言自带 HTTP 客户端即可（Python 用 `urllib`，零第三方依赖）
- 解析统一包络，`code != "ok"` 即视为失败
- 远程键与本地默认值合并，配置中心里只放必改键（如 `PG_PASSWORD`），其余走代码内默认值
- 错误显式兜底、绝不静默降级：连不上 → 提示启动 liteconf-server；HTTP 404 → 提示去控制台建 app/env；缺必填键 → 启动即报错退出
- 服务地址留环境变量覆盖口子（如 `LITECONF_URL`），方便换实例/端口调试

```python
url = f"{LITECONF_URL}/api/{APP}/{ENV}"      # http://localhost:8646/api/sql-pg-sqlalchemy/dev
with urllib.request.urlopen(url, timeout=3) as resp:
    body = json.loads(resp.read().decode("utf-8"))
cfg = {**DEFAULTS, **body["data"]["content"]}  # 远程键覆盖默认值；缺 PG_PASSWORD 则报错退出
```

### 命名建议

`App` 用服务名（如 `taskmon`），`Env` 用环境名（`dev`/`test`/`prod`），所有服务共用同一个 server 实例。名称只能含 `[a-zA-Z0-9_-]`。

## 项目结构

```
liteconf/
├── cmd/
│   ├── liteconf-server/   # server 入口（flags/日志/优雅退出）
│   └── liteconf/          # 调试 CLI 入口（组装：标准流 + 退出码）
├── internal/
│   ├── server/            # server 实现（internal 不对外）
│   │   ├── store.go       # 存储层：加载/原子写/版本元数据
│   │   ├── handler.go     # HTTP API（读/写/发现）与 /ui/ 路由注册
│   │   ├── watch.go       # 长轮询挂起与 close-broadcast 广播
│   │   ├── poller.go      # 外部编辑周期检测
│   │   ├── errors.go      # 统一响应包络与错误码
│   │   └── webui/         # Web 控制台（纯静态托管，无业务逻辑）
│   │       ├── webui.go   # go:embed 嵌入与 no-cache Handler
│   │       └── static/    # 内嵌前端：index.html / app.js / style.css
│   ├── cli/               # CLI 适配器（子命令分发/透传/契约导出）
│   │   ├── cli.go         # Run 分发与运行依赖注入点（Options）
│   │   ├── http.go        # http 子命令：curlie 透传编排
│   │   ├── mcp.go         # mcp 子命令：stdio MCP server 组装与 -server 参数
│   │   ├── catalog.go     # 命令契约目录（help 与 schema 的同源数据）
│   │   └── schema.go      # schema 子命令：契约目录 JSON 导出（tools + commands）
│   ├── mcp/               # MCP 适配器（官方 go-sdk，stdio server）
│   │   ├── mcp.go         # server 构建（工具注册）与 Run 入口
│   │   ├── api.go         # apiClient：HTTP 调 server /api/* 与统一业务错误
│   │   ├── tools.go       # 三个工具的输入/输出类型、handler、点路径下钻
│   │   └── view.go        # ToolSpecs：in-memory 读注册视图（供 schema 同源导出）
│   └── version/           # 版本号注入点（build.ps1 -ldflags -X）
├── client/                # SDK（公开路径，供业务 import）
│   ├── client.go          # 初始化与公开 API
│   ├── cache.go           # copy-on-write 快照缓存与点路径读取
│   ├── poll.go            # 长轮询循环 + 指数退避重连
│   └── callback.go        # OnChange 注册与异步派发
└── scripts/build.ps1      # 构建脚本，产物输出 build/
```

## 定位与边界

单实例部署（文件即数据库，无集群/主备）；无鉴权、无 TLS、无历史版本回滚；不校验配置 schema。控制台为最小 GUI 壳子：仅静态托管 + 页面交互，无登录鉴权、无自动保存、无乐观锁（保存即覆盖，last-write-wins），沿用内网可信环境定位。

按《CLI 工具开发标准》记录能力表：三个入口共用 server HTTP API 这一个数据面——Go 业务进程 import `client/` SDK（读优化 + 变更回调），AI 客户端走 `liteconf mcp`（MCP stdio），人调试走 `http` 透传 + Web 控制台。MCP 未暴露 watch 长轮询（理由见「MCP 工具」一节）；`http` 为纯终端透传，无合理 MCP 形态，不重复暴露。依赖偏离说明：调试 CLI 为 MCP 入口引入官方 go-sdk v1.8.0（第三方），server 与 client SDK 保持仅标准库；liteconf-server 二进制不包含 MCP 代码。
