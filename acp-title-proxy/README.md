# acp-title-proxy

Zed ACP (Agent Client Protocol) 智能标题增强工具。

## 核心设计哲学
为了**绝不破坏 Zed 官方 Agent Registry 的自动更新与版本演进**：
- **`settings.json` 保持 100% 官方默认**（保留 `"type": "registry"`），享受 Zed 官方自动拉取最新版本的核心能力；
- **通过外挂数据层对齐（Mode A）**：在不介入 stdio 进程管道的前提下，通过对齐 SQLite 数据层，无损注入智能标题。

---

## 解决的问题与双轨策略
1. **OpenCode**：自身后台已为会话生成了高质量标题（存入 `~/.local/share/opencode/opencode.db`），自动无感同步，**0 Token 成本、纯本地计算**；
2. **Google Antigravity (AGY)**：支持**多模型优先级回退链（Provider Fallback Chain）**：
   - **首选（Primary）**：`OpenCode Go` 网关的 `deepseek-v4.1-flash`（显式关闭 `reasoningEffort` 提速至 1s 内）；
   - **兜底（Fallback）**：`DeepSeek 官方 API` (`deepseek-flash`，即 DeepSeek-V4.1-Flash，显式关闭思考以提速至 1s 内)，在 OpenCode Go 网关超时或异常时自动无缝接管；
   - **零配密钥**：支持 `"api_key": "auto"` 自动读取本机 `~/.local/share/opencode/auth.json` 中的凭据。

---

## 运行模式

### 模式 A（推荐：零侵入、settings.json 不做任何修改、升级绝对安全）

#### 1. 一键单次同步 (One-shot Sync)
扫描 Zed 数据库中所有未命名会话，从 `opencode.db` 提取 OpenCode 标题，调用模型链为最近的 AGY 会话生成语义标题：
```powershell
.\bin\acp-title-proxy.exe sync
```

#### 2. 后台持续监听 (Background Watcher)
作为守护进程在后台运行（每 3 秒检测一次），一旦在 Zed 中发起新会话，自动补齐标题：
```powershell
.\bin\acp-title-proxy.exe watch
```
*(可使用 Windows 任务计划程序、`clictl` 或开机启动脚本在后台静默运行)*

> **标题判定规则（重要）**：代理判定"会话是否已命名"时：
> 1. 本地持久化存储命中 → 跳过；
> 2. `title_override` 非空（用户手动重命名）→ 尊重用户，跳过；
> 3. `title` 非空且**不是当前消息的占位副本** → 视为历史会话已有真实标题，跳过；
> 4. `title` 只是首条消息原文（含截断、文件引用前缀）→ 属于 Zed 的占位标题，
>    **必须继续生成**，否则标题生成会在首条消息上自我否定、永不执行。
>
> 第 3 条同时承担"重启后在旧会话发『继续』不得覆写标题"的防护。

#### 3. 故障排查
所有运行日志落盘在 `%APPDATA%\language_projects\acp-title-proxy\`，按写者角色分成三个文件：

| 文件 | 写者 | 说明 |
| --- | --- | --- |
| `acp-title-proxy.log` | 共享后端 daemon（顶层 `share`，单例） | 标题生成、模型调用、耗时、失败原因 |
| `acp-title-proxy-pool.log` | 预热池 daemon（顶层 `daemon`，单例） | worker 预热与回收 |
| `acp-title-proxy-client.log` | Zed 每个 panel 的客户端进程 | attach / 回退直连 / 直连模式的标题日志 |

Zed 不会转发 agent 子进程的 stderr，排查标题问题请直接看这些文件（**首选 `acp-title-proxy.log`**）。

轮转策略（两个 daemon 文件走标准 `lumberjack`）：
- 单文件超过 **5 MB** 自动轮转；
- 保留最近 **3 份**，超过 **14 天**自动删除；
- 归档一律 **gzip 压缩**，文件名形如 `acp-title-proxy-2026-11-01T10-38-15.923.log.gz`。

`acp-title-proxy-client.log` 由多个 panel 进程并发追加，Windows 下 Go 文件句柄不允许其他进程 rename，
因此改用「复制为带时间戳的 gzip 归档 → 原地截断」轮转（同样 5 MB / 3 份 / 14 天），
缺点只是归档名带进程号、极端并发下可能多出一两个历史份。

---

### 模式 B（备用：Stdio 管道透明代理）
若未来某些场景确需作为子进程管道运行：
```powershell
.\bin\acp-title-proxy.exe opencode
.\bin\acp-title-proxy.exe agy
```

---

### 共享后端（推荐：多个 agent panel 复用同一个 AGY worker）

`agy share` 把 AGY 后端变成常驻单例：Zed 每个 panel 只启动一个轻量客户端，
所有 panel 共享同一个 `agy_acp_server`，内存不再随 panel 数线性增长。
不带 `share` 的 `agy` 仍然保持原来的 1:1 预热池行为，随时可回退。

```powershell
.\bin\acp-title-proxy.exe share          # 启动共享 daemon（首个 agy share 也会自动拉起）
.\bin\acp-title-proxy.exe share-status   # 查看 worker / 会话 / 客户端数
.\bin\acp-title-proxy.exe share-stop     # 停止 daemon 并回收 worker 进程树
.\bin\acp-title-proxy.exe share-restart  # 重启
```

`settings.json` 的 `agent_servers` 新增（不要改官方 `antigravity-acp` registry 条目）：

```json
"Google Antigravity (auto-title)": {
  "type": "custom",
  "command": "D:/Users/language_projects/go_projects/acp-title-proxy/bin/acp-title-proxy.exe",
  "args": ["agy", "share"],
  "env": {
    "HTTP_PROXY": "http://127.0.0.1:7897",
    "HTTPS_PROXY": "http://127.0.0.1:7897",
    "NO_PROXY": "localhost,127.0.0.1,::1",
    "REQUESTS_CA_BUNDLE": "C:/Users/29580/AppData/Roaming/Zed/certs/antigravity-ca.pem",
    "SSL_CERT_FILE": "C:/Users/29580/AppData/Roaming/Zed/certs/antigravity-ca.pem"
  }
}
```

- 每个 panel 一个轻量客户端（约 10MB），只做 stdio ↔ TCP 转发；
- 共享 worker 实测 1 worker + 2 会话约 190MB；
- `agy share` 连不上 daemon 会尝试拉起，仍失败则回退直连；
- 标题生成与 `/skill:` 技能注入在共享模式下同样生效；
- `serve` 保留为内部别名，效果与 `agy share` 相同；
- 所有 panel 关闭后，daemon 空闲 `share.idle_ttl_minutes`（默认 30）分钟会自动退出并释放 worker/harness；
  下次打开 panel 时 `serve` 自动拉起 daemon（冷启动几秒），旧 thread 的历史仍从本地会话库回放。
- `share.max_sessions`（默认 12）限制的是**活跃会话**数，不是历史会话总数：
  只有真正发过 prompt、且在最近 `share.active_window_minutes`（默认 15）分钟内有消息往返的会话才计入配额。
  Zed 恢复面板时会批量 `session/load` 历史线程，这类只读回放完全不占名额，
  不会再出现「翻了几个旧对话就 Failed to Launch: max_sessions reached」。

---

### AGY 官方二进制自动更新（`agy update`）

在 Zed 中，使用自定义 `type: "custom"` 的 Agent 不会触发官方 Registry 的二进制下载逻辑。
`acp-title-proxy` 内置了与 Zed 官方哈希算法 100% 对齐的更新引擎，能够自动检测官方更新，
并将新版二进制静默解压到 Zed 官方目录规范（`%LOCALAPPDATA%\Zed\external_agents\registry\antigravity-acp\v_*`）中。

#### 1. 手动检查与更新
```powershell
.\bin\acp-title-proxy.exe agy update          # 检查并更新到最新版（带进度条）
.\bin\acp-title-proxy.exe agy update --check  # 仅检查版本，不执行下载
.\bin\acp-title-proxy.exe agy update --force  # 强制重新下载并解压当前版本
```

#### 2. 后台静默自动更新
在 `share` 后端常驻 daemon 运行时，后台将定期（默认每 4 小时）静默检测是否有新版发布：
- 启动延时 15 秒触发，**绝不抢占前台 UI 资源**；
- 发现新版自动拉取、校验 SHA-256 并解压入库；
- 自动清理更早的旧版本（默认保留最新的 2 份，节省磁盘空间）；
- 正在运行的旧 Worker 不会被强制掐断，将在空闲超时（`idle_ttl`）重启或下次打开面板时无缝切换至新版。

---

### 会话归属迁移（`agy threads`）

Zed 把每个会话的归属记在 `sidebar_threads.agent_id` 上，取值就是 `settings.json`
里 `agent_servers` 的键名。历史上由官方 `antigravity-acp` 创建的会话，可以把
`agent_id` 就地改写成 `Google Antigravity (auto-title)`：`session_id` 保持不变，
会话本体（`~/.gemini/antigravity-acp/conversations/<sid>.db`）两边共用，重开时
Zed 发 `session/load`，后端回放完整历史（agy 声明了 `loadSession: true`）。

```powershell
# 看分布（只读，Zed 开着也能看）
.\bin\acp-title-proxy.exe agy threads list

# 过户（默认 dry-run；--from/--to 可省略，默认即 官方 → auto-title）
.\bin\acp-title-proxy.exe agy threads adopt
.\bin\acp-title-proxy.exe agy threads adopt --apply     # 执行前先完全退出 Zed

# 只归档、不改归属
.\bin\acp-title-proxy.exe agy threads archive --agent antigravity-acp --apply

# 精确回迁：只把「快照里属于 antigravity-acp」的会话改回去，
# 原本就在代理名下的、以及迁移后新建的会话保持不动
.\bin\acp-title-proxy.exe agy threads adopt `
  --from "Google Antigravity (auto-title)" --to antigravity-acp `
  --snapshot "$env:APPDATA\language_projects\acp-title-proxy\backups\threads-<时间戳>\db.sqlite"
```

`--snapshot` 用于「已经迁走、想按原样退回」：Zed 库里没有来源标记，`agent_id` 一旦
改写就无法反推归属，只能拿迁移前的快照当证据（`--snapshot-agent` 指定在快照中按哪个
`agent_id` 判定，默认取 `--to`）。不传 `--snapshot` 时行为不变，仍是全量过户。

- 写库前必须**完全退出 Zed**（含托盘），否则改动会被内存缓存覆盖；
- 写前用 `VACUUM INTO` 做一致性快照（自动合并 WAL），落在
  `%APPDATA%\language_projects\acp-title-proxy\backups\threads-<时间戳>\db.sqlite`；
- 没有 `session_id` 的空 thread 会被跳过，需要时用 `archive` 单独处理；
- 迁移**不降低内存**：每个打开的会话仍有独立 harness，省掉的是 `agy_acp_server`
  的重复实例；收益是历史会话统一由共享 daemon 承载、标题统一管理。

#### 双向切换速查（官方 ⇄ 代理）

两个条目可以反复来回切：`session_id` 与对话内容都不动，只改 `agent_id`。
每次 `--apply` 都会自动存一份改动前快照，切错了能退回上一步。

```powershell
cd D:\Users\language_projects\go_projects\acp-title-proxy

# 每次切换前必须为 0（Zed 完全退出，含托盘）
@(Get-Process Zed -ErrorAction SilentlyContinue).Count
```

切到代理 `Google Antigravity (auto-title)`：

```powershell
.\bin\acp-title-proxy.exe agy threads adopt --from antigravity-acp --to "Google Antigravity (auto-title)" --apply
.\bin\acp-title-proxy.exe agy threads archive --agent antigravity-acp --apply
.\bin\acp-title-proxy.exe agy threads list
```

切回官方 `antigravity-acp`：

```powershell
.\bin\acp-title-proxy.exe agy threads adopt --from "Google Antigravity (auto-title)" --to antigravity-acp --apply
.\bin\acp-title-proxy.exe agy threads archive --agent "Google Antigravity (auto-title)" --apply
.\bin\acp-title-proxy.exe agy threads list
```

- `adopt` 换归属，有 `session_id` 的会话全部搬走；
- `archive` 把搬不走的空白 thread（新建后还没发过消息）归档，让源条目彻底归零；
- 想先看再动手就去掉 `--apply`，dry-run 会打印条数与样例。

**三个容易误判的点**

1. Zed 里那个 `New Google Antigravity (auto-title) Thread` 是「新建会话用哪个 agent」
   的选择器，不会随数据库切换而改变；切完方向后要在面板顶部手动选一次 agent，
   否则新建的会话仍落在上次那个条目下；
2. 会话列表是**所有 agent 混排**，且两个条目的图标可能相同，肉眼分不出归属——
   用 `agy threads list --archived active --limit 15` 确认；
3. 切换**不会改写历史标题**：`session/load` 不触发标题生成，旧会话标题保持原样；
   要测自动标题只能新建会话。

**备份清理**（每份约 900KB，反复切换会堆积，保留最近 5 份）：

```powershell
Get-ChildItem "$env:APPDATA\language_projects\acp-title-proxy\backups" -Directory |
  Sort-Object Name -Descending | Select-Object -Skip 5 |
  Remove-Item -Recurse -Force
```

---

## AGY 进程内存泄漏与孤儿回收（`agy reap`）

### 现象

Zed 每打开/切换一个 AGY 会话，就会拉起一整套 AGY 进程：

| 进程 | 典型内存 | 说明 |
| --- | --- | --- |
| `agy_acp_server.exe` | 12 MB（主）/ 180 MB（直连） | ACP 服务端 |
| `localharness_external.exe` | 120–160 MB / 个 | 每个 ACP 会话一个沙箱 harness |

客户端（Zed panel / `acp-title-proxy` 客户端）退出后，这两个进程在 Windows 上**不会自行退出**。
反复切换会话会在后台累积十几个 180 MB 的僵尸进程，把内存推到 90% 以上；
同时 `~/.gemini/antigravity-acp/conversations` 会涌入大量空会话库。

### 回收命令

```powershell
.\bin\acp-title-proxy.exe agy reap --dry-run   # 只扫描，列出可清理的孤儿
.\bin\acp-title-proxy.exe agy reap             # 立即清理
```

判定规则（宁漏不误杀）：

1. 直接父进程已退出（PID 不存在）→ 孤儿；
2. 父进程就是 `acp-title-proxy` / Zed / 其他宿主 → 视为活跃，**绝不动**；
3. 父进程本身也是 AGY 进程（父子一起泄漏的形态）→ 递归判断。

### 根治：Job Object 进程级兜底

所有会拉起 AGY 后端的路径（共享 daemon、直连回退、预热池 worker、stdio 代理）
都会在 `cmd.Start()` 之后立即把后端进程绑定到 Windows Job Object
（`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`，见 `internal/procguard`）：

- 宿主进程正常退出、崩溃、甚至被任务管理器强杀，**内核都会连带终止整棵进程树**
  （`agy_acp_server` + 所有 `localharness_external`），不再依赖 agy 自己是否干净退出；
- 实测：只杀 daemon 本身（不杀进程树），后端在 1 秒内全部消失；
- attach 失败（例如被上层 Job 限制）只打印一行警告并降级，不影响启动，
  此时由上面的 `agy reap` 巡检兜底。

### 自动巡检

`agy share` 常驻 daemon 启动后会自动开启孤儿巡检（复用 `reaper.enabled` 与
`reaper.check_interval_minutes`，默认每 5 分钟一次，启动 30 秒后先跑一次），
发现孤儿直接清理并写入 `acp-title-proxy.log`：

```
[OrphanReaper] 发现 3 个孤儿 AGY 进程（父进程已退出），合计约 430 MB，已清理 3 个
```

### 使用建议

- **不要把十几个 AGY 历史会话同时开成 panel**：每个 panel 都是一套独立 harness，
  共享后端只复用 `agy_acp_server`，不共享 harness；
- 历史会话尽量统一过户到 `Google Antigravity (auto-title)`（见上节 `agy threads adopt`），
  避免 Zed 用官方 `antigravity-acp` 直连拉起 180 MB 的独立后端；
- 长时间不用 AGY 时关掉 panel，daemon 会在 `share.idle_ttl_minutes`（默认 30 分钟）
  后整体退出并释放全部 harness。

---

## 配置文件 (`config.json`)
放置在 `bin/config.json` 或同级目录下（已由 `.gitignore` 自动排除）：
```json
{
  "opencode": {
    "db_path": "%USERPROFILE%/.local/share/opencode/opencode.db",
    "poll_interval_ms": 1500,
    "max_polls": 8
  },
  "agy": {
    "providers": [
      {
        "name": "opencode-go",
        "enabled": true,
        "base_url": "https://opencode.ai/zen/go/v1",
        "api_key": "auto",
        "model": "deepseek-v4.1-flash",
        "reasoning_effort": "none",
        "timeout_ms": 4000
      },
      {
        "name": "deepseek-official",
        "enabled": true,
        "base_url": "https://api.deepseek.com/v1",
        "api_key": "YOUR_DEEPSEEK_API_KEY",
        "model": "deepseek-flash",
        "reasoning_effort": "none",
        "timeout_ms": 4000
      }
    ]
  },
  "auto_update": {
    "enabled": true,
    "check_interval_hours": 4,
    "keep_versions": 2
  }
}
```

## 编译
```powershell
go build -o bin/acp-title-proxy.exe ./cmd/acp-title-proxy
```
