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
所有运行日志（含每次模型调用、耗时、失败原因）落盘到：
```
%APPDATA%\language_projects\acp-title-proxy\acp-title-proxy.log
```
Zed 不会转发 agent 子进程的 stderr，排查标题问题请直接看该文件。

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
- `serve` 保留为内部别名，效果与 `agy share` 相同。

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
  }
}
```

## 编译
```powershell
go build -o bin/acp-title-proxy.exe ./cmd/acp-title-proxy
```
