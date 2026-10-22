# liteconf

轻量配置中心：`.env` 的替代品、极简版 Apollo。单实例 Go 服务托管各应用各环境的 JSON 配置文件，配套 Go client SDK 实现配置热更新与变更回调，并提供**桌面控制台（GUI）**与**命令行（CLI）**两条通道。server 与 client SDK 零第三方依赖，仅 Go 标准库；桌面壳使用 Wails v3，CLI 的 MCP 入口引入官方 [go-sdk](https://github.com/modelcontextprotocol/go-sdk) v1.8.0。

定位**内网/本机可信环境**：HTTP 明文、无鉴权，请勿暴露公网。

## 特性

- **双通道产物**：`bin/liteconf-gui.exe` 只开桌面控制台（托盘常驻、窗口几何记忆）；`bin/liteconf-cli.exe` 只跑终端子命令（`serve` / `http` / `mcp` / `schema` / `url`），互不混淆
- **文件即数据库**：配置按 `configs/{app}/{env}.json` 二级目录存储，直接用编辑器改文件就能生效
- **版本化**：每次变更版本号 +1，内存维护版本/哈希/修改时间元数据
- **长轮询热更新**：客户端携带已知版本挂起，变更秒级感知（Apollo 同款机制）
- **外部编辑检测**：绕过 API 直接改文件，服务周期扫描（默认 5s）自动感知并广播
- **原子写入**：临时文件 + rename，失败不留半截配置
- **SDK 容错**：断线指数退避自动重连，故障期间继续读最后一份可用配置
- **内嵌 Web 控制台**：GUI 窗口与浏览器共用同一套 `/ui/` 页面，配置浏览、查看、双模式编辑（JSON 文本 / 键值表）与新建；静态资源经 `go:embed` 随二进制分发
- **动态端口 + 自动发现**：GUI 与 `serve` 默认绑定 `127.0.0.1:0`，实际地址写入 `runtime.json`，`liteconf url` 与 MCP 子命令自动发现，无需约定端口
- **MCP 入口**：`bin/liteconf-cli.exe mcp` 启动 stdio MCP server（官方 go-sdk v1.8.0，协议基线 2025-11-25），AI 客户端可直接发现、读取、写入配置
- **调试 CLI**：`http` 子命令把参数原样透传给 curlie（HTTPie 语法 + curl 引擎）调试 API，`schema` 导出契约目录（MCP 工具 + CLI 命令同源）
- **零依赖数据面**：服务端与 client SDK 仅用标准库，GUI 产物才包含 Wails/WebView2 依赖

## 快速开始

### 构建

```powershell
cd go_projects\liteconf
uv run scripts/build.py                 # dev     → .\bin\liteconf-gui-dev.exe + .\bin\liteconf-cli-dev.exe
uv run scripts/build.py --release       # release → .\bin\liteconf-gui.exe     + .\bin\liteconf-cli.exe
```

release 只在 HEAD 精确命中 `liteconf/v*` tag 且工作区干净时产出，其余情况一律 dev（`--release` 条件不足时退出码 2）。PE 版本资源由 `go-winres` 注入，GUI 与 CLI 的「文件说明」不同。

### 启动桌面控制台（推荐）

```powershell
.\bin\liteconf-gui-dev.exe
```

- 双击零黑框，窗口显示内嵌 WebUI；关闭窗口不退出进程，托盘常驻，双击托盘图标可重建窗口。
- 默认绑定 `127.0.0.1:0` 动态端口，实际地址写入 `%APPDATA%\language_projects\liteconf\<dev|prod>\runtime.json`，供 `liteconf url` 与 MCP 自动发现。
- 托盘菜单：打开控制台 / 在默认浏览器中打开 / 开机自启（仅 release）/ 彻底退出。
- 窗口尺寸、位置与最大化状态记忆在 `window.json`，重开精确恢复。

### 启动无窗口服务（脚本 / 服务器）

```powershell
.\bin\liteconf-cli-dev.exe serve                  # 默认 127.0.0.1:0，地址打印在 stdout
.\bin\liteconf-cli-dev.exe serve -addr :8646      # 固定端口（需要外部固定地址时使用）
```

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-addr` | `127.0.0.1:0` | HTTP 监听地址（`:0` = 动态探测端口） |
| `-root` | `%APPDATA%\language_projects\liteconf\configs` | 配置根目录（取不到 APPDATA 回退 `~/.language_projects/liteconf/configs`） |
| `-poll` | `5s` | 外部编辑检测间隔 |
| `-open` | `false` | 启动后打开系统默认浏览器 |

`serve` 与 GUI 打开的是**同一个 HTTP Handler**：根路径 302 到 `/ui/`，`/api/*` 提供 REST。GUI 日志写在 `%APPDATA%\language_projects\liteconf\<dev|prod>\liteconf.log`（同时输出 stderr），无控制台时是唯一事后取证入口。

### 命令行通道（liteconf-cli.exe）

`gui` 产物不接受任何子命令；所有终端能力都在 CLI 产物中：

```powershell
.\bin\liteconf-cli-dev.exe url                      # 输出运行中实例的地址（GUI/serve 自动发现）
.\bin\liteconf-cli-dev.exe serve                    # 无窗口启动 WebUI + API
.\bin\liteconf-cli-dev.exe http GET :8646/api/app1/dev
.\bin\liteconf-cli-dev.exe http PUT :8646/api/app1/dev key=value
.\bin\liteconf-cli-dev.exe mcp                      # stdio MCP server（自动发现，回退 :8646）
.\bin\liteconf-cli-dev.exe mcp -server http://192.168.1.10:8646
.\bin\liteconf-cli-dev.exe schema                   # 输出契约目录（JSON）：tools + commands
```

`--hidden` / `--tray` 在 CLI 通道退化为无窗口 `serve`，供开机自启脚本使用。

`url` 优先读取当前通道的 `runtime.json`，未命中再尝试另一通道，并校验进程存活；实例已退出时自动回退默认地址 `http://127.0.0.1:8646`，不会把陈旧地址交给调用方。

`http` 把全部参数原样透传给 PATH 中的 curlie，stdin/stdout/stderr 直通、不经 shell 包裹，`--help` 等参数归 curlie 而非 liteconf。退出码：0 成功；1 运行失败（如 curlie 缺失，stderr 给出安装指引）；2 调用参数错误；`http` 子命令透传 curlie 的退出码。版本号由构建脚本 `-ldflags -X` 注入（`bin/liteconf-cli-dev.exe --version` 查看）。

### 依赖

- `http` 子命令需要 curlie：`go install github.com/rs/curlie@latest`
- GUI 需要 Microsoft Edge WebView2 Runtime（Windows 11 自带；缺失时 GUI 自动降级为无窗口服务并尝试打开浏览器）
- 构建需要 `go-winres`：`go install github.com/tc-hib/go-winres@latest`

### MCP 接入

`mcp` 子命令是常驻 stdio MCP server：stdin/stdout 专用于协议，启动诊断写 stderr。AI 客户端（opencode、Claude Desktop 等）在 `mcpServers` 中以子进程方式接入：

```json
{
  "mcpServers": {
    "liteconf": {
      "command": "D:\\path\\to\\liteconf-cli.exe",
      "args": ["mcp"],
      "env": { }
    }
  }
}
```

不传 `-server` 时自动发现：先读当前通道的 `runtime.json`（GUI 或 `serve` 正在运行），未命中再尝试另一通道，校验进程存活后连接其实际端口；未发现时回退 `http://127.0.0.1:8646`。因此 GUI 使用动态端口也能被 MCP 正确连上。`-server` 显式指定时以参数为准；服务未启动时工具调用返回 `server_unreachable`。MCP 数据面与 Web 控制台、curlie 同一地位（走 `/api/*`），client SDK（`client/`）供 Go 业务进程 import，三者互不替代。

## MCP 工具

| 工具 | 对应 API | 输入 | 结构化输出 | 行为标注 |
|---|---|---|---|---|
| `liteconf.discovery` | `GET /api/discovery` | 无 | `{"ok":true,"data":{"apps":[{app,envs:[{env,version}]}]}}` | 只读 |
| `liteconf.config.get` | `GET /api/{app}/{env}` | `app`,`env` 必填；`path` 可选点路径（如 `db.host`，未命中 `path_not_found`） | `{"ok":true,"data":{app,env,version,content}}` | 只读 |
| `liteconf.config.put` | `PUT /api/{app}/{env}` | `app`,`env`,`content`（完整 JSON 对象，整体覆盖写） | `{"ok":true,"data":{app,env,version}}` | 破坏性、非幂等（每次版本 +1） |

- 业务失败返回 `isError=true` 且结构化错误 `{"ok":false,"error":{"code","message"}}`；错误码透传 server 包络（`not_found` / `invalid_name` / `invalid_json` / `internal`），客户端侧新增 `server_unreachable`、`path_not_found`。
- `liteconf schema` 输出的 `tools` 与 MCP 实际注册同源（in-memory transport 读注册视图，全分页遍历）；`commands` 为 CLI 命令契约。
- **不暴露 watch 长轮询**：挂起 30~120s 与 MCP 请求-响应的等待预算/取消语义冲突；等价替代是重读 `liteconf.config.get` 对比 `version`。

## 契约变更记录

- `liteconf schema` 自本版起输出 `{"name","version","tools":[...],"commands":[...]}`，**移除存量 `interface:"cli"` 字段**（提供 MCP 入口后不再适用《CLI 工具开发标准》5.5 的 cli 例外）；tools 与 MCP 注册同源，新增 `mcp` 子命令。
- 双通道改造后 CLI 命令契约新增 `serve` 与 `url`；`http` / `mcp` / `schema` / `version` / `help` 语义不变。
- go.mod 的 go 指令自 1.25.0 升至 **1.26.0**（Wails v3.0.0-beta.33 硬要求）；服务端与 client SDK 业务代码仍仅标准库。
- `liteconf-server.exe` 已移除，由 `bin/liteconf-cli.exe serve` 取代（同一 Handler、同一 WebUI）；旧启动脚本请改命令，配置数据目录不变。

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

示例（PowerShell，固定端口 `serve -addr :8646` 或旧部署）：

```powershell
.\bin\liteconf-cli-dev.exe http GET :8646/api/discovery
.\bin\liteconf-cli-dev.exe http PUT :8646/api/app1/dev timeout:=30 db:='{\"host\":\"127.0.0.1\"}'
```

动态端口下可先取地址再拼接：

```powershell
$base = .\bin\liteconf-cli-dev.exe url
.\bin\liteconf-cli-dev.exe http GET "$base/api/discovery"
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

`GET /ui/` 托管内嵌的静态控制台页面（HTML/JS/CSS 经 `go:embed` 编译进二进制，无外部文件依赖，响应均带 `Cache-Control: no-cache`）。`GET /` 会 302 到 `/ui/`，因此 GUI 窗口、`serve` 打印的地址与浏览器直接访问根路径都能落到完整工作台。控制台只做渲染与交互，全部业务操作（列表/读取/写入/新建）通过上述 `/api/*` 接口完成。

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
    // 服务固定端口（如 `bin/liteconf-cli serve -addr :8646`）时直接写地址；
    // GUI 动态端口场景请读取 runtime.json 的 url，或给 serve 指定固定端口。
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
- 错误显式兜底、绝不静默降级：连不上 → 提示启动 liteconf（GUI 或 `bin/liteconf-cli serve`）；HTTP 404 → 提示去控制台建 app/env；缺必填键 → 启动即报错退出
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
│   └── liteconf/          # 单包双入口：main.go(!cli) GUI 通道 / cli.go(cli) Console 通道
├── internal/
│   ├── app/               # Wails 桌面壳：gui / window_state / autostart / 双图标 / 单实例探测
│   ├── appenv/            # 运行环境：通道数据目录、共享 configs、runtime.json、日志
│   ├── server/            # 服务端（纯标准库）
│   │   ├── store.go       # 存储层：加载/原子写/版本元数据
│   │   ├── handler.go     # HTTP API、根路径重定向与 /ui/ 路由注册
│   │   ├── service.go     # Store + Broadcaster + Poller 聚合（GUI 与 CLI serve 共用）
│   │   ├── watch.go       # 长轮询挂起与 close-broadcast 广播
│   │   ├── poller.go      # 外部编辑周期检测
│   │   ├── errors.go      # 统一响应包络与错误码
│   │   └── webui/         # Web 控制台（纯静态托管，无业务逻辑）
│   ├── cli/               # CLI 适配器（子命令分发/透传/契约导出/服务启动）
│   │   ├── cli.go         # Run 分发与运行依赖注入点（Options）
│   │   ├── serve.go       # serve 子命令：动态端口、runtime.json、优雅退出
│   │   ├── http.go        # http 子命令：curlie 透传编排
│   │   ├── mcp.go         # mcp 子命令：stdio MCP server 组装与地址自动发现
│   │   ├── catalog.go     # 命令契约目录（help 与 schema 的同源数据）
│   │   └── schema.go      # schema 子命令：契约目录 JSON 导出（tools + commands）
│   ├── mcp/               # MCP 适配器（官方 go-sdk，stdio server）
│   ├── version/           # 版本号与 BuildMode/Channel 注入点（build.py -ldflags -X）
│   └── ...
├── client/                # SDK（公开路径，供业务 import）
├── scripts/
│   ├── build.py           # 双通道构建：dev/release × GUI/CLI + go-winres 资源段
│   └── render_icon.py     # release 专属图标渲染（Pillow，确定性输出）
├── winres/                # gui.json / cli.json：PE VERSIONINFO + manifest + 图标模板
├── icon.png / icon.ico    # release 图标（构建生成，不入库）
├── info.json              # 无 git tag 时的版本基线
├── wails.exe.manifest     # 4 段式 WinSxS 清单
└── bin/                   # 构建产物（gitignore）
```

运行时数据（`%APPDATA%\language_projects\liteconf\`）：

- `configs/{app}/{env}.json`：配置数据，**dev 与 prod 共用**（切通道不丢配置）
- `dev/` 与 `prod/`：日志、`window.json`、WebView2 缓存与 `runtime.json`，按通道隔离

## 定位与边界

单实例部署（文件即数据库，无集群/主备）；无鉴权、无 TLS、无历史版本回滚；不校验配置 schema。控制台为最小 GUI 壳子：仅静态托管 + 页面交互，无登录鉴权、无自动保存、无乐观锁（保存即覆盖，last-write-wins），沿用内网可信环境定位。

能力入口统一到同一份 `/api/*` 数据面：

| 入口 | 产物 / 通道 | 适用场景 |
|---|---|---|
| 桌面控制台 | `bin/liteconf-gui.exe`（GUI） | 本机日常浏览、编辑、新建配置；托盘常驻、窗口几何记忆 |
| 浏览器 / 无窗口服务 | `bin/liteconf-cli.exe serve`（Console） | 服务器、脚本、远程浏览器访问；动态端口或 `-addr` 固定端口 |
| Go 业务进程 | import `client/` SDK | 读优化 + 长轮询热更新 + 变更回调 |
| AI 客户端 | `bin/liteconf-cli.exe mcp`（stdio MCP） | 发现/读取/写入配置；自动发现运行中实例 |
| 人调试 | `bin/liteconf-cli.exe http` 透传 + Web 控制台 | HTTPie 语法快速验证 API |

`http` 为纯终端透传，无合理 MCP 形态，不重复暴露；MCP 未暴露 watch 长轮询（挂起 30~120s 与 MCP 请求-响应的等待预算/冲突），等价替代是重读 `liteconf.config.get` 对比 `version`。依赖偏离说明：GUI 产物引入 Wails v3 与 WebView2；CLI 的 MCP 入口引入官方 go-sdk v1.8.0；服务端与 client SDK 保持仅标准库，`bin/liteconf-cli.exe serve` 不包含 Wails 代码。
