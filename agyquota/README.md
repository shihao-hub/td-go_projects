# agyquota

**Antigravity 模型配额查询 CLI** —— 无论是否打开 Antigravity IDE，直接在终端中毫秒级查询 Google AI Pro 在 Antigravity 中的模型配额：Gemini 与 Claude/GPT 两个模型桶的 Weekly / Five Hour 剩余百分比和重置时间。

支持双通道独立查询：
- **`--agy`**：基于官方 **Antigravity CLI (`agy`)** 驱动，查询终端与桌面 GUI 账号配额；执行完毕强制回收进程树，绝无后台残留；
- **`--zed`**：基于 **Zed (`antigravity-acp`)** 独立凭据直连 Google 官方接口，带 50 分钟内存级安全缓存与 1:1 官方 User-Agent 伪装。

遵循《CLI 工具开发标准 v2（daemon 架构）》：单二进制双角色（`agyquota serve` + 客户端）、HTTP+JSON 唯一契约、buildID 握手；CLI 人读输出 + `--json` 信封 + `mcp` stdio 桥 + `schema` 契约导出。

## 构建与安装

要求 Go 1.25+：

### 1. 使用 scripts 构建脚本（推荐）

```powershell
# 构建 agyquota.exe（版本与 buildID 由 git describe --tags --always --dirty 注入）
.\scripts\build.ps1

# 发布构建：仅干净 tag 检出（describe 形如 agyquota/v1.2.3）允许通过
.\scripts\build.ps1 -Release

# 构建并自动安装部署至系统 PATH
.\scripts\install.ps1
```

### 2. 手动构建

```powershell
$describe = git describe --tags --always --dirty
go build -trimpath -ldflags "-s -w -X agyquota/internal/buildinfo.Version=$describe" -o agyquota.exe ./cmd/agyquota
```

## 快速上手

```powershell
# 1. 查询官方 CLI / 桌面端账号配额（主力安全模式，随用随杀）
agyquota --agy

# 2. 查询 Zed 插件账号配额（独立凭据模式，带本地安全缓存）
agyquota --zed
```

输出示例（人读，真实数据）：

```text
Antigravity 模型配额 [Antigravity CLI (agy)]（查询于 2026-09-23 09:55:23）
Gemini Models
  Gemini Models                5h          95%  4小时40分钟后刷新
  Gemini Models                weekly      86%  5天12小时后刷新

Claude and GPT models
  Claude and GPT models        5h         100%  4小时59分钟后刷新
  Claude and GPT models        weekly      96%  5天15小时后刷新
```

## 命令与参数一览

| 命令 / 参数 | 说明 |
|---|---|
| `agyquota --agy` | 查询官方 CLI / 桌面端账号配额（daemon 内调用 `agy -p /usage`，自动杀进程） |
| `agyquota --zed` | 查询 Zed (`antigravity-acp`) 账号配额（读取本地凭据与缓存） |
| `agyquota --agy --json` | JSON 信封输出：`{"ok":true,"data":…}` / `{"ok":false,"error":{…}}` |
| `agyquota --zed --raw` | 打印底层接口原始响应（排障/调试用） |
| `agyquota serve` | 前台启动 daemon（`--bind` 默认 127.0.0.1、`--port` 默认 17625、`--idle-timeout` 默认 0=不退出） |
| `agyquota stop` | 停止正在运行的 daemon（未运行时不报错） |
| `agyquota mcp` | 以 stdio MCP server 运行（工具名 `agyquota.quota.get`，支持 `source: agy|zed`） |
| `agyquota schema` | 导出与 tools/list 同源的 MCP 工具契约目录 |
| `--host host[:port]` | 指定 daemon 目标地址（缺省端口 17625；优先级最高） |
| `--help` / `--version` | 自描述，不触发网络请求、不启动 daemon |

退出码：`0` 成功、`1` 业务失败、`2` 参数错误（如未传 `--agy`/`--zed`）。

## daemon 架构与工作流

查询不再由 CLI/MCP 进程内直接执行业务，而是统一交给后台 daemon（`agyquota serve`）：

- **地址发现优先级**：`--host` → 环境变量 `AGYQUOTA_HOST` → 地址文件 → 默认 `127.0.0.1:17625`；
- **生产构建自动拉起**：buildID 的 describe 部分为干净 tag（如 `agyquota/v1.2.3`）且未显式指定地址时，CLI/MCP 会用自身 exe 自动拉起 daemon（Windows `DETACHED_PROCESS`，无窗口、不抢焦点、关闭终端不退出），等待就绪后继续请求；
- **开发构建不拉起**：describe 带 `-g<hash>`/`-dirty` 时连不上直接报错，提示先运行 `agyquota serve`。开发走双终端：

  ```powershell
  # 终端 1：前台跑 daemon，日志在眼前，Ctrl+C 即停
  agyquota serve
  # 终端 2：客户端测试
  agyquota --agy --json
  ```

- **空闲退出**：自动拉起的 daemon 在无在途请求且空闲超过 30 分钟后优雅退出并清理地址文件（下一次调用会自动重新拉起）；前台 `agyquota serve` 不设空闲退出；
- **buildID 握手**：客户端连接后核对双方 buildID，不等时报错并提示 `agyquota stop` 后重试（捕获「升级后旧 daemon 仍在跑」「只重启了一边」）；
- **单飞与取消**：daemon 内同源并发查询合并为一次取数；请求级取消/超时只终止该请求的在途操作（含 agy 子进程），不终止 daemon。

### 数据目录（`%APPDATA%\language_projects\agyquota\`）

取不到 `APPDATA` 时回退 `~/.language_projects/agyquota/`：

| 文件 | 说明 |
|---|---|
| `daemon.json` | 地址文件（addr/version/buildID/pid/startedAt）；监听成功后原子写入，退出即删除（陈旧文件由新 daemon 覆盖 + 客户端重读兜底） |
| `daemon.log` / `daemon.log.1` | 自动拉起时的 daemon 日志；单文件超过 1 MiB 时在拉起前轮转为 `.1` |
| `.quota_token_cache.json` | Zed Access Token 与配额历史缓存（约 58 分钟安全期） |
| `bin\agy_silent.exe` | agy 的 GUI 子系统静默副本（消除新建标签与抢焦点） |

Zed 凭据目录（`~/.gemini/antigravity-acp/`）对本工具**只读**。

### 发布流（git tag 驱动）

```text
git tag -a agyquota/v1.2.3 -m "agyquota v1.2.3"   # 父仓多项目共存，tag 带项目前缀
git push --tags
.\scripts\build.ps1 -Release                       # 仅干净 tag 检出可构建
```

`git describe --tags --always --dirty` 一份来源四处使用：开发指纹、握手 buildID（附 Unix 秒时间戳）、`--version` 输出、升级检测。

## 架构与安全设计

1. **官方 CLI 安全隔离 (`--agy`)**：
   - 自动探测系统 PATH 或 `%LOCALAPPDATA%\agy\bin\agy.exe`；
   - 运行完成后，无论成功与否均强制终结进程树（Windows Job Object），绝不在后台挂起孤儿进程；
   - daemon 以 detached 方式运行时，静默副本不可用的回退路径附加 `CREATE_NO_WINDOW`，不产生可见窗口。
2. **Zed 独立接口与防风控设计 (`--zed`)**：
   - 凭据来源：`~/.gemini/antigravity-acp/acp_token.json`；
   - **50 分钟安全缓存**：自动将获取的 Access Token 与过期时间缓存至本项目自有数据目录，在有效期内直接复用，彻底杜绝高频刷新；凭据目录（`~/.gemini/`）对本工具只读，不写入任何文件；
   - **1:1 官方 User-Agent**：严格复刻 Zed ACP 官方客户端指纹，规避脚本特征识别。
3. **分层隔离**：业务核心（`internal/service`）只编译进 daemon；CLI 与 MCP 桥是 `internal/client` 之上的薄壳，依赖方向由 `go list -deps` 断言。

## 故障排查

| 错误码 | 含义 | 处理 |
|---|---|---|
| `source_required` | 未指定 `--agy` 或 `--zed` | 根据要查询的对象选择 `--agy` 或 `--zed` 参数 |
| `agy_not_found` | 未检测到 Antigravity CLI | 确认已安装 `agy` 并在 PATH 或 `%LOCALAPPDATA%\agy\bin\agy.exe` 中 |
| `agy_execute_failed` | `agy -p "/usage"` 执行报错 | 检查网络代理或先独立执行 `agy -p "hello"` 排查 |
| `zed_execute_failed` | Zed 凭据读取或请求失败 | 检查 `~/.gemini/antigravity-acp/acp_token.json` 是否存在 |
| `response_parse_failed` | 输出格式发生变化 | 运行 `--raw` 查看原始响应 |
| `daemon_unreachable` | daemon 未运行或不可达（含开发构建不自动拉起） | 运行 `agyquota serve`；或检查 `--host`/`AGYQUOTA_HOST` 指向 |
| `daemon_start_failed` | 自动拉起 daemon 失败 | 查看 `%APPDATA%\language_projects\agyquota\daemon.log`；或手动 `agyquota serve` 排查 |
| `build_mismatch` | 客户端与 daemon 不是同一次构建 | 按提示执行 `agyquota stop` 后重试 |

## 手工验收记录（2026-09-25）

daemon 化改造（spec `02-daemon-architecture`）实测结果：

| 项 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` / `GOOS=linux go build ./...` | 通过 |
| `go list -deps ./internal/cli ./internal/mcp` 含 `internal/service` | 无（依赖方向断言通过） |
| 开发构建（describe 带 `-dirty`）不拉 daemon | `--agy --json` → 退出码 1、`daemon_unreachable`、提示先 `agyquota serve`；无 daemon 进程 |
| 生产构建自动拉起（等价注入 `agyquota/v0.0.0.<时间戳>`） | `--agy --json` 自动 detached 拉起 daemon、写地址文件 `127.0.0.1:17625`、查询成功；`stop` 后无残留进程，地址文件删除 |
| buildID 握手 | 不同 buildID 客户端被拒（`build_mismatch`，含双方指纹与 stop 建议）；`stop` 豁免可恢复 |
| 并发单飞 | 同源两并发请求 `fetchedAt` 完全一致，仅 leader 输出进度（agy 仅执行一次） |
| 空闲退出 | `serve --idle-timeout 300ms` 无在途请求自动退出并删地址文件；前台 serve 默认不退出 |
| SSE 进度与结果 | 进度经 stderr（`[agyquota] ` 前缀），结果包络正确；`--raw` 原始响应 2888 字节完整 |
| MCP 真实进程 | `initialize` + `tools/list` 发现 `agyquota.quota.get`；无 daemon 时调用返回 `isError`（daemon_unreachable）；daemon 运行时调用成功（structuredContent，2 桶） |
| `schema` 离线导出 | 成功，`source` 保留 `enum: [agy, zed]`（注：0.2.3 因 jsonschema-go v0.4 升级实际 panic，本次为修复后契约） |
| `--host` 非法 / `AGYQUOTA_HOST` 非法 / 显式地址不可达 | 分别为退出码 2 `bad_args` / 退出码 1 `daemon_unreachable` / 退出码 1，均不本地拉起 |
| `stop --json`（未运行） | `{"ok":true,"data":{"stopped":false}}`，退出码 0 |
| `-Release` 守卫 | dirty describe 拒绝构建 |
| 数据目录与只读边界 | 新增/修改文件仅在 `%APPDATA%\language_projects\agyquota\`（`daemon.json`/`daemon.log`/缓存/`bin\`）；Zed 凭据目录无本工具写入 |

待提交后补做（需要干净检出）：

- 干净检出打临时 annotated tag `agyquota/v0.0.0` + `.\scripts\build.ps1` 的生产验收，完成后删除临时 tag；
- 静默副本缺失回退路径的「无可见窗口」人工核对（需故障注入删除 `bin\agy_silent.exe`）。
