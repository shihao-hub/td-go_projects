# agy-share 实现设计：ACP 多会话共享后端

状态：设计稿（未写代码）
依据：阶段 0 探针验证 PASS（见 `%TEMP%\agy-share-probe\REPORT.md`）
目标：一个常驻 `agy_acp_server` 进程承载多个 ACP session，3 会话常驻 ≤ 250MB

---

## 1. 背景

- Zed 每个 agent panel 启动一个 `agy_acp_server.exe`，每会话约 190MB；N 个会话线性叠加。
- 阶段 0 已证明：**一条 stdio 连接可原生承载多个 session**，反向请求 9/9 带 `sessionId`，1 worker + 2 session 仅 189–195MB。
- 因此不需要逆向 `language_server`，只需在 ACP 层做多路复用。

## 2. 协议事实（实测，必须遵守）

| 事实 | 影响 |
| :--- | :--- |
| `initialize` 返回 `agentInfo`、`authMethods`、`sessionCapabilities`（list/additionalDirectories/resume） | 可缓存 initialize 结果 |
| `session/new` 每次返回独立 `sessionId` + `modes` + `configOptions` + `models` | session 配置按会话独立，不能共用 |
| `session/update` 全部带 `sessionId` | 通知可按 session 路由 |
| 反向请求 `session/request_permission`、`fs/read_text_file` 全部带 `sessionId` | 可按 session 路由到对应 client |
| `session/cancel` 是 **notification**，不是 request | 按通知转发；发成 request 会 `-32601` |
| 权限请求不响应会阻塞该 prompt | client 断开/拒绝必须触发清理，否则会话悬挂 |
| 工具面包含终端 execute、子代理、workflow、MCP | mux 不需要理解工具语义，只做路由 |

## 3. 总体架构

```
Zed panel 1 ──stdio──> agy-share serve ──TCP──┐
Zed panel 2 ──stdio──> agy-share serve ──TCP──┤
Zed panel 3 ──stdio──> agy-share serve ──TCP──┤
                                              ▼
                                    agy-share daemon
                                      ├─ ClientRegistry   (client 连接 / capabilities)
                                      ├─ SessionRegistry  (clientID ↔ backendSessionID)
                                      ├─ Router           (双向消息分流)
                                      └─ Worker           (1 个 agy_acp_server.exe)
```

- **serve**：Zed 每 panel 一个，~10MB；只做 stdio ↔ TCP 转发 + 握手，不含业务逻辑。
- **daemon**：单例；拥有 worker、session 表、路由与生命周期。
- **worker**：`agy_acp_server.exe`，由 daemon 启动，独占 stdin/stdout。

## 4. 关键设计决策

### 4.1 共享粒度：1 worker / N session

- 默认 1 个 worker；`max_sessions` 限制并发 session 数。
- 超限策略 `overflow`：
  - `reject`（默认）：新 `session/new` 返回错误，Zed 显示可读错误；
  - `spawn`：启动第二个 worker（内存回到线性增长，仅作应急）。
- 不按 cwd 拆 worker：实测不同 cwd 的 session 可在同一 worker 共存。

### 4.2 initialize：init-on-first-client

- daemon 启动后**不**立即 initialize。
- 第一个 client 的 `initialize` 原样转发给 worker，缓存：
  - `initializeResult`
  - `clientCapabilities` 的规范化哈希
- 后续 client：
  - 哈希相同 → 直接返回缓存 result（不转发）；
  - 哈希不同 → 默认拒绝并提示（避免 worker 以错误 capabilities 运行）；配置 `allowMixedCapabilities=true` 时启动第二个 worker。
- 原因：worker 的 initialize 决定它是否走 client 的 fs/terminal 回调，必须与 client 实际能力一致。

### 4.3 session 映射

- daemon 维护 `sessionId → clientConn` 与 `clientConn → set<sessionId>`。
- `session/new` 响应中的 `sessionId` 原样透传给 client（UUID 全局唯一，实测无冲突）。
- 若未来出现冲突，daemon 生成别名并重写双向消息中的 `sessionId`；当前不实现，仅记录告警。

### 4.4 反向请求路由

```
worker→daemon 消息:
  if method != "" && id != nil:      # 反向 request
      sid = params.sessionId
      if sid == nil: 回 -32602 并记录；不猜测
      conn = sessionId→conn[sid]
      if conn == nil: 回 -32602 "unknown session"
      else: 转发给 conn；client 的 response 按原 id 回 worker
  else if method != "":             # notification
      sid = params.sessionId
      if sid != nil: 转发给对应 conn
      else: 广播（仅限白名单方法），否则丢弃并记录
  else:                              # response
      按 id 回给发起方（client 或 daemon 内部请求）
```

- 权限请求的 `optionId` 由 client 选择后原样回传，daemon 不改写。
- 文件路径不做改写；`fs/read_text_file` 的 `path` 原样转发。

### 4.5 cancel 与关闭

- `session/cancel` 是 notification：按 sessionId 转发给 worker。
- client 断开：
  1. 标记该 client 的所有 session 为 orphan；
  2. 尝试发 `session/cancel`（若有 in-flight prompt）；
  3. 若 ACP 支持 `session/close`（待验证），发送之；否则按 `idle_ttl` 回收映射；
  4. **不杀 worker**。
- worker 崩溃：
  1. 所有 client 收到 JSON-RPC error（code -32000，message "agent worker exited"）；
  2. daemon 以指数退避重启 worker（1s/2s/5s/10s，上限 3 次）；
  3. client 重连后重新 initialize；旧 session 标记失效。

### 4.6 连接协议（serve ↔ daemon）

复用 `acp-title-proxy/internal/pool` 的风格，但独立端口：

```
1. serve 连接 127.0.0.1:<port>
2. 发一行 JSON: {"cmd":"attach","proto":1,"clientPid":1234}
3. daemon 回: {"status":"ok"} 或 {"status":"error","msg":"..."}
4. 之后为纯 ACP 行协议（newline-delimited JSON-RPC 2.0）
```

- 握手超时 2s；失败时 serve 回退为直连启动独立 worker（保证 Zed 永远可用）。
- daemon 端口默认 47600，可用 `share.port` 覆盖。

### 4.7 标题/技能注入（可选）

- 若复用 `acp-title-proxy` 的 Titler/SkillManager：作为 mux 的 middleware，挂在 daemon 的 inbound/outbound 路径，按 session 维护状态。
- 若独立项目：先不做标题注入，后续以插件形式接入。
- 注入不得改变 `sessionId` 与消息顺序。

## 5. 状态与数据结构

```text
Client { id, conn, capabilitiesHash, sessions set, connectedAt }
Session { sessionId, clientId, cwd, state: active|orphan|closed, lastActiveAt }
Worker  { cmd, stdin, stdout, pid, initialized, initResult, startedAt }
Daemon  { listener, clients map, sessions map, worker, config, stats }
```

状态机：

```
Session: created → active → orphan → closed
Worker:  starting → ready → crashed → restarting → ready
Client:  attached → detached
```

## 6. 故障处理矩阵

| 故障 | 行为 |
| :--- | :--- |
| 反向请求无 sessionId | 回 -32602，记录；不跨会话猜测 |
| 反向请求 sessionId 未知 | 回 -32602 "unknown session" |
| client 半途断开 | session 标记 orphan；cancel；TTL 回收 |
| client 发来未知 sessionId | 回 -32602 |
| worker 启动失败 | daemon 返回 attach 错误；serve 回退直连 |
| worker 崩溃 | 广播错误；退避重启；session 失效 |
| daemon 崩溃 | serve 的 TCP 断开 → 回退直连（可选自动重启 daemon） |
| 达到 max_sessions | 按 overflow 策略 reject/spawn |
| capabilities 冲突 | 默认拒绝；可选第二 worker |

## 7. CLI 与配置

```text
agy-share daemon            启动常驻 daemon（可后台）
agy-share serve             供 Zed 每个 panel 调用
agy-share status            查看 worker/session/内存
agy-share stop              停止 daemon 与 worker
agy-share restart           重启 daemon
```

```json
{
  "share": {
    "enabled": true,
    "port": 47600,
    "max_sessions": 6,
    "overflow": "reject",
    "idle_ttl_minutes": 30,
    "allow_mixed_capabilities": false,
    "fallback_direct": true
  }
}
```

## 8. 测试计划

**单元**
- sessionId → client 路由；未知 sessionId 拒绝；
- 反向请求无 sessionId 拒绝；
- capability 哈希分组；
- orphan TTL 回收。

**集成（脚本模拟 ACP client）**
- 3 个 client 并发 `session/new` + `session/prompt`，输出不串；
- 每个 client 收到自己的 `session/request_permission`，回不同 optionId，互不影响；
- `session/cancel` notification 只影响目标会话；
- client 中途断开，其他会话继续；
- kill worker 后 daemon 重启，新会话可用。

**验收**
- 3 会话总 WorkingSet ≤ 250MB；
- 关闭 2 会话后回落到 ≤ 210MB；
- 连续开关 10 次无单调增长；
- 单 worker 崩溃恢复 < 10s。

## 9. 与现有项目的关系

- **落点 A（新建 agy-share）**：复制 `internal/pool`（Worker/KillProcessTree/连接协议）与 `internal/protocol` 的消息类型；不依赖 acp-title-proxy 运行时。
- **落点 B（扩展 acp-title-proxy）**：新增 `internal/mux` 包；现有 `daemon` 保持 1:1 预热池，新增 `share` 模式；标题注入复用现有 Titler。
- 两种落点共用本设计；差别只在包路径与发布物。

## 10. 未决问题

- [ ] `session/close` 是否存在、语义如何？（阶段 0 只验证了 list/resume 能力声明）
- [ ] `session/resume` / `session/load` 在共享 worker 下的行为？
- [ ] Zed 是否会在 `initialize` 后发 `authenticate`？多 client 时是否需要缓存 auth 状态？
- [ ] `session/request_permission` 的 response 是否必须原样带回 `optionId` 与 `toolCallId`？
- [ ] 多 client capabilities 不同的真实场景（Zed vs 其他 ACP client）是否需要支持？
- [ ] 是否需要在 daemon 层做 per-session 并发限流（防止一个长任务饿死其他会话）？
