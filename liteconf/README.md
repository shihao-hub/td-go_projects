# liteconf

轻量配置中心：`.env` 的替代品、极简版 Apollo。单实例 Go server 托管各应用各环境的 JSON 配置文件，配套 Go client SDK 实现配置热更新与变更回调。零第三方依赖，仅 Go 标准库。

定位**内网/本机可信环境**：HTTP 明文、无鉴权，请勿暴露公网。

## 特性

- **文件即数据库**：配置按 `configs/{app}/{env}.json` 二级目录存储，直接用编辑器改文件就能生效
- **版本化**：每次变更版本号 +1，内存维护版本/哈希/修改时间元数据
- **长轮询热更新**：客户端携带已知版本挂起，变更秒级感知（Apollo 同款机制）
- **外部编辑检测**：绕过 API 直接改文件，server 周期扫描（默认 5s）自动感知并广播
- **原子写入**：临时文件 + rename，失败不留半截配置
- **SDK 容错**：断线指数退避自动重连，故障期间继续读最后一份可用配置
- **内嵌 Web 控制台**：浏览器访问 `/ui/` 即可完成配置浏览、查看、双模式编辑（JSON 文本 / 键值表）与新建，静态资源经 `go:embed` 随二进制分发
- **零依赖**：server 与 SDK 均只用标准库，`go build` 即得单文件二进制

## 快速开始

### 构建与启动

```powershell
cd go_projects\liteconf
.\scripts\build.ps1              # 产物 build/liteconf-server.exe
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

### 写入配置

```powershell
curl.exe -X PUT http://localhost:8646/api/app1/dev -H "Content-Type: application/json" --data-binary "@config.json"
# 或直接编辑 %APPDATA%\language_projects\liteconf\configs\app1\dev.json（6 秒内被感知）
```

### 接口测试

Bruno collection 位于父仓库 `docs/go_projects/liteconf/brunos/`（OpenCollection YAML 格式），选 `local` 环境按编号顺序执行。

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

### 命名建议

`App` 用服务名（如 `taskmon`），`Env` 用环境名（`dev`/`test`/`prod`），所有服务共用同一个 server 实例。名称只能含 `[a-zA-Z0-9_-]`。

## 项目结构

```
liteconf/
├── cmd/liteconf-server/   # server 入口（flags/日志/优雅退出）
├── internal/server/       # server 实现（internal 不对外）
│   ├── store.go           # 存储层：加载/原子写/版本元数据
│   ├── handler.go         # HTTP API（读/写/发现）与 /ui/ 路由注册
│   ├── watch.go           # 长轮询挂起与 close-broadcast 广播
│   ├── poller.go          # 外部编辑周期检测
│   ├── errors.go          # 统一响应包络与错误码
│   └── webui/             # Web 控制台（纯静态托管，无业务逻辑）
│       ├── webui.go       # go:embed 嵌入与 no-cache Handler
│       └── static/        # 内嵌前端：index.html / app.js / style.css
├── client/                # SDK（公开路径，供业务 import）
│   ├── client.go          # 初始化与公开 API
│   ├── cache.go           # copy-on-write 快照缓存与点路径读取
│   ├── poll.go            # 长轮询循环 + 指数退避重连
│   └── callback.go        # OnChange 注册与异步派发
└── scripts/build.ps1      # 构建脚本，产物输出 build/
```

## 定位与边界

单实例部署（文件即数据库，无集群/主备）；无鉴权、无 TLS、无历史版本回滚；不校验配置 schema。控制台为最小 GUI 壳子：仅静态托管 + 页面交互，无登录鉴权、无自动保存、无乐观锁（保存即覆盖，last-write-wins），沿用内网可信环境定位。按《CLI 工具开发标准》记录例外：本项目形态为服务 + 库，无管理命令面，故不配套 CLI 与 MCP。
