# clictl 开发计划

## 项目概述

一个 Windows 单文件 CLI 工具：注册任意 exe，`clictl run <名>` 透传启动，启动过程被接管并记录。所有管理命令**永远输出 JSON**（lark-cli 的 CLI/GUI 分离思想），存储用 SQLite。

核心需求（均来自用户原话）：

- "一个 cli 工具，这个工具有自己的命令"
- "x list 列出来注册的 exe；x run ... 启动，就和直接启动 exe 一样，只是启动过程交给管理器"
- "启动次数、上次启动时间、耗时等可以做，未来还会拓展"
- "永远都返回 json"
- "不是 config.json 而是 sqlite"，放 `%AppData%`

## 关键决策

| 决策 | 内容 |
|---|---|
| 语言 | **Go**（理由见"关键技术点"） |
| 命名 | 项目 `clictl`，命令 `clictl`（构建产物 `clictl.exe`，PATH 已验证无冲突） |
| 管理对象 | v1 仅 `.exe`（.cmd/.bat shim 推二期） |
| 数据位置 | `%AppData%\clictl\clictl.db`（取不到回退 `~/.clictl/`） |

## 项目结构

```
go_projects/clictl/
├── cmd/clictl/main.go            # 入口：子命令分发
├── internal/cli/commands.go      # add/rm/list/info/run 各命令实现
├── internal/cli/output.go        # JSON 包络输出（ok/error、--pretty）
├── internal/store/store.go       # SQLite 打开/迁移/CRUD/统计
├── internal/store/meta.go        # meta 白名单校验器（key 约束 + 4KB 硬限）
├── internal/runner/runner.go     # 透传启动 + 启动记录回写
├── scripts/build.ps1             # 构建 + 版本注入
├── README.md / .gitignore
父仓镜像文档：docs/go_projects/clictl/clictl.md
```

## 核心数据结构

```sql
CREATE TABLE IF NOT EXISTS tools (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL UNIQUE,   -- 调用名，统一小写存储
  path        TEXT NOT NULL UNIQUE,   -- 绝对路径，Clean 后
  description TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'active'
              CHECK (status IN ('active','invalid')),  -- 上次校验时的文件状态
  meta        TEXT,                   -- 扩展 JSON；应用层白名单 + 4KB 硬限，见关键技术点
  added_at    TEXT NOT NULL           -- RFC3339
);
CREATE TABLE IF NOT EXISTS launches (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  tool_id     INTEGER NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
  started_at  TEXT NOT NULL,          -- RFC3339
  duration_ms INTEGER,                -- NULL = 未正常结束
  exit_code   INTEGER,
  pid         INTEGER                 -- v1.2.0：仅 start 写入；run 记录恒 NULL；未闭环 start = pid 非空且 duration 为空
);
CREATE INDEX IF NOT EXISTS idx_launches_tool
  ON launches(tool_id, started_at DESC);
```

```go
// list/info 的 JSON 数据形状
type Tool struct {
    ID          int64           `json:"id"`
    Name        string          `json:"name"`
    Path        string          `json:"path"`
    Description string          `json:"description"`
    Status      string          `json:"status"`                // active / invalid，输出前现场 stat 刷新
    Meta        json.RawMessage `json:"meta,omitempty"`        // 白名单约束的扩展字段，空则省略
    AddedAt     string          `json:"added_at"`
    SizeBytes   int64           `json:"size_bytes"`            // 运行时 stat
    LaunchCount int64           `json:"launch_count"`          // JOIN launches 统计
    LastLaunch  string          `json:"last_launch,omitempty"` // 从未启动则省略
}

// meta 校验器（internal/store/meta.go）：应用层强制约束
// - key 白名单，v1 仅两项：
//     source: string，≤ 64 字节，来源标记（cargo / go / npm / pip / manual / custom ...自由文本）
//     tags:   []string，≤ 8 个，每项 ≤ 32 字节，去重后小写存储
// - 白名单之外的 key 一律拒绝（error code: meta_unknown_key）
// - 类型不匹配报 meta_invalid；序列化后 UTF-8 总字节数 > 4096 报 meta_too_large
func ValidateMeta(raw []byte) (json.RawMessage, error)

// JSON 包络（学 lark-cli）：成功 {"ok":true,"data":...}
//                        失败 {"ok":false,"error":{"code":"...","message":"..."}}
```

## CLI 设计

| 命令 | 说明 | 退出码 |
|---|---|---|
| `clictl add <path> [--name N] [--desc D] [--meta JSON]` | 注册；name 默认=文件名去 `.exe` 小写化；meta 过白名单校验 | 0 / 1(conflict/meta_invalid) |
| `clictl rm <name>` | 删除注册（级联删其 launches） | 0 / 1(not_found) |
| `clictl set <name> --meta JSON` | 整体替换 meta（同过白名单校验） | 0 / 1(not_found/meta_invalid) |
| `clictl list [--status active|invalid] [--running]` | 全部工具，按 launch_count 降序、name 升序；可按状态过滤；`--running` 只看后台活实例（附 running_pids，与 --status 互斥） | 0 |
| `clictl info <name>` | 详情 + 最近 10 条启动 + 累计耗时 + 后台运行状态（running.alive/pids） | 0 / 1(not_found) |
| `clictl run <name> [args...]` | **透传启动**：stdin/stdout/stderr/退出码全部直通 | =子进程码 / 127(未注册) |
| `clictl start <name> [args...]` | **后台分离启动**（DETACHED）：点火即走返回 pid；已有活实例附 already_running；未注册/失效=127（错误 JSON 走 stdout） | 0 / 127 |
| `clictl stop <name>` | 全量终止后台活实例（taskkill /T /F 树杀）+ 杀后复探确认 + 闭环记录（exit_code=1 强杀约定值） | 0 / 1(有杀失败) |
| `clictl --version` | 版本号（也是 JSON） | 0 |

- 全局 `--pretty`：缩进 JSON 供人读；默认紧凑单行
- 管理命令的 stdout 永远是合法 JSON；`clictl run` 的 stdout 永远只属于子进程，管理数据一律不混入
- `run` 失败（未注册/文件失效）发生在子进程输出之前，错误 JSON 走 stderr，不污染 stdout

## 核心模块设计

**store**（`internal/store/store.go`）

- 职责：打开 DB（WAL 模式 + busy_timeout=2s）、建表迁移、CRUD、统计
- 签名：
  - `Open() (*Store, error)`
  - `(*Store) AddTool(name, path, desc string, meta json.RawMessage) (Tool, error)`
  - `(*Store) RemoveTool(name string) (int64, error)`
  - `(*Store) SetMeta(name string, meta json.RawMessage) error`
  - `(*Store) RefreshStatus(id int64, valid bool) error`
  - `(*Store) ListTools(status string) ([]Tool, error)`（status 空串 = 不过滤）
  - `(*Store) GetTool(name string) (Tool, error)`
  - `(*Store) InsertLaunch(toolID int64, startedAt time.Time) (int64, error)`
  - `(*Store) FinishLaunch(id int64, durMs int64, exitCode int) error`
  - `(*Store) RecentLaunches(toolID int64, n int) ([]Launch, error)`
  - `(*Store) TotalDuration(toolID int64) (count int64, totalMs int64, err error)`
- 规则：name 统一 `strings.ToLower` 后比较/存储；path `filepath.Clean` 后去重；meta 写入前必须过 `ValidateMeta`，读出后原样透传不做二次解释
- **status 刷新策略**：add 时初始 `active`；之后每次 list/info/run 触碰到该工具都现场 `os.Stat`——JSON 输出的 status 永远是实时结论，且与落库值不一致时同步回写（写入频率低，无需异步）

**runner**（`internal/runner/`）

- 职责：透传启动 + 后台启动 + 探活 + 停止 + 记账
- `Run` 流程：`lookup(name)`（查注册 + 现场校验 + 相似名建议，run/start/stop 共用）→ `InsertLaunch(now)` → `exec.Command(path, args...)`，`Stdin/Stdout/Stderr = os.Std*` **不经任何 shell 包裹** → `signal.Notify` 忽略父进程 Ctrl+C（子进程同控制台组自然收到并退出）→ `Wait` → `FinishLaunch` → `os.Exit(子进程退出码)`
- `Start` 流程：`lookup` → 探活已有实例（already_running 提示）→ `InsertLaunch` → `exec.Command` + `SysProcAttr{DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP}`（无控制台不闪黑框，与 clictl 生命周期解耦）→ `SetLaunchPID(pid)` → 立即返回，永不闭环
- `Stop` 流程：`lookup` → `UnfinishedStarts` → `FilterAlive`（三重探活）→ 逐个 `taskkill /PID x /T /F`（树杀子进程）→ 复探确认 → `FinishLaunch(dur, 1)`（强杀约定值）
- `Alive(pid, expectPath)` 探活三重校验：OpenProcess 存在性（只认 ERROR_ACCESS_DENIED 为"存在但权限不足"，INVALID_PARAMETER 判死）+ GetExitCodeProcess != STILL_ACTIVE(259) 判已退出（进程退出后若有外部句柄引用，进程对象不销毁，OpenProcess 仍成功——必须查终止态）+ QueryFullProcessImageName 路径比对（防 PID 回收复用）
- Ctrl+C 场景：父进程吞掉信号等子进程死，回写 duration/exit_code 后再退，记录不丢

**cli**（`internal/cli/`）

- `Emit(data any)` → `{"ok":true,"data":...}`；`Fail(code, msg string)` → `{"ok":false,"error":{...}}` + exit 1
- 子命令解析用标准库 `flag` 的子命令模式，不引 CLI 框架

## 实现步骤（分阶段）

### Phase 1：骨架 + 存储 + add/rm/list（估 1.5h）

- [x] 1. 初始化 go.mod / 目录 / .gitignore / README 骨架
- [x] 2. store：Open + WAL + 建表（含 status/meta 列）+ CRUD + status 现场刷新回写
- [x] 3. meta 校验器：白名单（source/tags）+ 类型/长度约束 + 4KB 硬限
- [x] 4. output 包络 + add（--meta）/ rm / list（--status 过滤）/ set 命令 + --pretty

验收标准：

- `clictl add` → `clictl list` → `clictl rm` 全链路 JSON 正确
- 重复 name/path 报 `conflict` 错误码
- DB 落在 `%AppData%\clictl\clictl.db`
- add 后 status=active；手动删掉 exe 文件后 `clictl list` 显示 invalid，且 `list --status invalid` 能过滤出该条
- meta 含白名单外 key / 类型错 / 超 4KB 分别报 `meta_unknown_key` / `meta_invalid` / `meta_too_large`
- `go vet ./...` 干净

### Phase 2：run 透传 + 启动记账（估 1.5h）

- [x] 5. runner：透传 + launches 插入/回写 + Ctrl+C 忽略
- [x] 6. run 接入 main + 未注册/失效错误路径

验收标准：

- `clictl run <名> --version` 输出与直接执行完全一致
- 交互程序（如 `clictl run python` 进 REPL）可用
- 退出码透传
- Ctrl+C 退出后该条 launch 的 duration_ms/exit_code 已回写
- list 的 launch_count/last_launch 正确

### Phase 3：info + 收尾（估 1h）

- [x] 7. `clictl info`：详情 + 最近启动 + 累计耗时
- [x] 8. build.ps1（-ldflags 版本注入）、README、父仓文档镜像、CHANGELOG

验收标准：

- build.ps1 产出单 `clictl.exe`
- `clictl --version` 输出 JSON 版本号
- 全部命令 JSON 输出格式一致

### Phase 4：智能提示（v1.1.0）

- [x] 9. 相似名建议：`internal/runner/suggest.go`（前缀 > 子串 > Levenshtein，纯标准库）+ 表驱动单测；`run` 未注册时 stderr 错误附 `suggestions` 字段（仅 run，退出码仍 127）
- [x] 10. `completion` 子命令：`powershell` 输出补全脚本（raw 文本例外）；`--install`/`--uninstall` 写入/移除 `$PROFILE`（conda-init 风格标记区块、幂等、Get-Command 守卫）；`names` 输出全部工具名
- [x] 11. 补全脚本行为：只绑定 clictl 命令名；第 1 位置补全子命令名；run/rm/info/set 后第 1 位置补全工具名（排除光标处正在输入的 wordToComplete）；run 透传段无候选；try/catch 静默失败

验收标准：

- `clictl run zedub` → suggestions=["zedhub"]，exit 127
- `go test ./...` 通过（12+ 用例）
- PS 5.1：脚本语法/注册/11 个位置判定用例通过；install/uninstall 幂等；带 profile 会话启动无报错

### Phase 5：后台启动 + PID 探活 + 全量停止（v1.2.0）

- [x] 12. store：launches 加 `pid` 列（新库 schema 自带 / 存量库 Open 时 PRAGMA 检测 + ALTER 自动迁移，SQL 存档父仓 migrations/）；`Launch` 加 PID 字段；`SetLaunchPID` / `UnfinishedStarts`
- [x] 13. runner：`start.go`（DETACHED 分离启动 + already_running）、`alive.go`（三重探活）+ 表驱动单测、`stop.go`（taskkill /T /F 全量树杀 + 复探确认 + 闭环回写 exit_code=1）、run/start/stop 共用 `lookup`
- [x] 14. cli：`start`/`stop` 子命令（错误走 stdout、未注册/失效=127）；`list --running`（与 --status 互斥）；`info` 加 running 字段；help 更新；补全纳入 start/stop
- [x] 15. 文档：README / CHANGELOG / 父仓镜像使用指南 / 父仓 migrations 存档；build 1.2.0

验收标准（已全部实测通过）：

- 存量库（无 pid 列）升级：自动加列、旧数据完好
- `start` 立即返回 pid；二次 start 附 already_running=true
- `list --running` 显示 running_pids；`info` running.alive 正确
- `stop` 全杀（多实例一次清空）、复探确认、记录闭环（duration + exit_code=1）；无实例时 already_stopped
- `run` 行为零回归（透传/退出码/前置错误走 stderr）
- 探活单测：真实路径比对、已退出进程判死、PID 复用路径不匹配判死

## 技术依赖

| 依赖 | 用途 | 理由 |
|---|---|---|
| modernc.org/sqlite | SQLite 驱动 | **纯 Go 无 CGO**：Windows 下无需 gcc，交叉编译不受限；本工具写入频率极低，性能足够 |
| golang.org/x/sys/windows | Win32 探活 API（OpenProcess/QueryFullProcessImageName） | 原为 sqlite 传递依赖，v1.2.0 提升为直接依赖，零新增体积 |
| 标准库 flag / os/exec / os/signal / encoding/json / database/sql | 其余全部 | 零 CLI 框架，单二进制最小化 |

## 关键技术点

- **为什么 Go 不 Rust/Python**：`clictl run` 在每次工具调用的热路径上，原生启动毫秒级（PyInstaller onefile 每次解压几百 ms 直接出局）；纯 Go SQLite 免 CGO；go_projects 已有 8 个同栈项目，exe-launcher/taskmon-go 沉淀的 exec 与 Windows 控制台经验直接复用。Rust 技术上同样可行，但对这种 CRUD+exec 工具迭代速度吃亏
- **透传不经 shell**：直接 `exec.Command(exePath, args...)`，避免 `cmd /c` 的引号地狱；Windows argv 转义由 Go `syscall.EscapeArg` 处理
- **Ctrl+C 记账不丢**：父进程 `signal.Notify` 吞 SIGINT，同控制台的子进程照常收到并退出，父进程 `Wait` 返回后回写再退出
- **WAL + busy_timeout**：多个 `clictl run` 并发时写 launches 不互相阻塞
- **status 双层机制**：落库 `status` 记录"上次校验结论"，但 JSON 输出前一律现场 `os.Stat` 刷新并回写——用户看到的永远是实时真相，落库值只服务于 `--status` 过滤和冷启动概览；CHECK 约束兜底防止脏值入库
- **meta 为何允许 JSON 字段**：扩展属性（来源、标签，未来还有别名/环境注入等）会持续演进，为避免频繁 ALTER TABLE 采用单 JSON 列；但按仓库约定施加硬约束——**应用层 key 白名单**（未知 key 直接拒绝，不让 meta 沦为垃圾抽屉）+ **序列化后 ≤ 4KB 硬限**（防单行膨胀）+ 每项值长度上限，所有约束集中在 `ValidateMeta` 单点收口

## 后续扩展（二期，本期不做）

- `clictl scan <dir>` 批量收编；`.cmd/.bat/.ps1` shim 支持（npm 那批）
- **`clictl rpc` JSON-RPC 模式**（stdin NDJSON，zedhub 式）——飞书文档思想的完全体，GUI/AI 客户端届时零成本接入
- 标签体系、`clictl stats` 全局统计、exe 版本资源探测、PATH 体检（残留/断供/命中优先级）

## 注意事项

- `%AppData%` 取不到回退 `~/.clictl/`；DB 损坏时输出 JSON 错误而非 panic
- run/start 前置校验失败绝不向 stdout 写管理 JSON（run 场景），保住"run 的 stdout 只属于子进程"约定；start/stop 是管理命令，错误 JSON 走 stdout 但退出码保持 127（与 run 跨命令一致）
- meta 是 TEXT 存 JSON 字符串（SQLite 无 JSONB），不建基于 meta 的 SQL 查询——所有 meta 检索/过滤都在应用层做，避免 JSON 查询性能陷阱
- 版本号唯一来源 = 构建命令 `-ldflags -X`（taskmon-go 同款约定）
- 本项目属 go_projects 子仓（monorepo），不单独 `git init`；提交格式 `clictl:<type>: <subject>`
- `completion powershell|names` 输出 raw 文本是 JSON 约定例外：消费者是 shell 补全脚本，输出即协议（同 run stdout 例外先例）；`--install`/`--uninstall` 动作型命令仍输出 JSON
- 补全脚本（内嵌 Go raw string）禁止使用反引号；升级脚本时旧安装块由 `$PROFILE` 安装行动态拉取最新版，无需重新 install
- 表结构变更三同步：schema 常量（新库）+ `migrate()` ALTER（存量库）+ 父仓 `docs/go_projects/clictl/migrations/` SQL 存档，缺一不可
- start 面向 GUI/托盘/服务类：DETACHED 无控制台，console 程序无输出能力（要看输出用 run）；Win11 商店化 stub（notepad/mspaint）自退出后真实进程 PID 与记录不符，属已知局限
- 探活已知局限：进程退出码恰为 259（STILL_ACTIVE 哨兵值）会误判为存活；SysWOW64/System32 路径重定向会误判为已退出；概率极低，接受
- stop 的 taskkill /T 树杀依赖父子快照，极端竞态（PID 复用窗口）可能波及无关子进程——与 taskkill 本身行为一致，接受

---
**最后更新：** 2026-09-12
**作者：** Claude & User
**版本：** v1.5（v1.4 = Phase 5 后台启动/探活/停止，发布为 1.2.0）
