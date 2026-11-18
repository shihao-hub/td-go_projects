# ccquota

只读渲染 cc-switch 里已启用供应商的额度：一条命令或一个本地页面，不必打开 cc-switch 窗口。

## 数据来源（全部只读）

| 内容 | 位置 |
|---|---|
| 供应商与用量查询开关 | `~/.cc-switch/cc-switch.db`（SQLite，`mode=ro` 打开） |
| OpenCode Go / DeepSeek / Command Code 的 Key | 上述库的 `providers.settings_config` |
| Codex 订阅 | `~/.codex/auth.json` 的 `tokens.access_token` |
| Kimi Code | `~/.kimi-code/credentials/*.json` 的 `access_token` |
| Antigravity | `agy -p "/usage" --output-format json`（与 IDE 展示同源） |

不刷新、不回写任何 token；不向 `~/.cc-switch` 或任何凭据目录写入文件。
自有数据（额度缓存、runtime 地址、日志）只落 `%APPDATA%\language_projects\ccquota\<dev|prod>\`。

## 用法

### 桌面（托盘）

双击 `bin\ccquota-gui-dev.exe`：无黑框窗口，关窗收进托盘，托盘菜单可「打开窗口 / 退出」；
重复启动不会开第二个实例，而是把已有窗口唤到前台。

### 命令行

```powershell
ccquota list                 # 人读列表（默认读 5 分钟缓存）
ccquota list --refresh       # 强制访问上游
ccquota list --json          # 机器输出：{"ok":true,"data":{...}}
ccquota list --app codex     # 只看某个应用
ccquota web --open           # 本地 WebUI（随机端口，地址打印在 stdout）
ccquota url                  # 输出运行中的 WebUI 地址
ccquota doctor               # 分层自检：库 / 凭据 / 实例
ccquota schema               # JSON 契约（零 I/O）
```

双击 `ccquota-dev.exe`（无参数、独占控制台）等价于 `ccquota web --open`，方便直接看界面。

退出码：`0` 成功、`2` 参数错误、`1` 其他失败。

## 支持的查询器（P0）

| 查询器 | 端点 | 窗口 |
|---|---|---|
| `opencode_go` | `https://opencode.ai/zen/go/v1/usage` | 5 小时 / 周 / 月 |
| `command_code` | `https://api.commandcode.ai/alpha/{whoami,billing/credits,billing/subscriptions,usage/summary}` | 5 小时 / 周 / 月 |
| `deepseek_balance` | `https://api.deepseek.com/user/balance` | 余额 |
| `codex_wham` | `https://chatgpt.com/backend-api/wham/usage` | 5 小时 / 周 |
| `kimi_coding` | `https://api.kimi.com/coding/v1/usages` | 5 小时 |
| `agy_quota` | Antigravity CLI `/usage` | 5 小时 / 周（按模型分组） |

查询失败时卡片保留上次成功值并标注错误，不会整屏变红。

## 构建

```powershell
cd D:\Users\language_projects\go_projects\ccquota
uv run scripts/build.py              # dev → bin/ccquota-dev.exe + bin/ccquota-gui-dev.exe
uv run scripts/build.py --release    # release → ccquota.exe + ccquota-gui.exe（需精确 tag + 干净工作区）
```

两个产物分工：`ccquota.exe` 是控制台通道（CLI、脚本、Agent 用它）；`ccquota-gui.exe` 是
`-H windowsgui` 的窗口通道（双击零黑框，托盘常驻），忽略除 GUI 语义外的参数。

## 已知限制

- 只读：不提供切换供应商、不写 cc-switch 配置。
- 官方订阅类 token 过期时不刷新，只提示重新登录。
- 只覆盖上表 6 个查询器；自定义 JS 脚本与其余内置模板留待后续。
- `command_code` 与 `opencode_go` 是未公开文档化的端点，响应形态可能变化，解析已按窗口逐项容错。
- 当 cc-switch 正在运行且库内含 hot journal 时，只读连接无法回滚，会自动复制一份快照到
  `%APPDATA%\language_projects\ccquota\<dev|prod>\snapshot.db` 再读，原库不被写入。
