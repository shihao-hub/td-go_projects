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

---

### 模式 B（备用：Stdio 管道透明代理）
若未来某些场景确需作为子进程管道运行：
```powershell
.\bin\acp-title-proxy.exe opencode
.\bin\acp-title-proxy.exe agy
```

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
