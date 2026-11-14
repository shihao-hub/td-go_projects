---
name: mail2feishu
description: QQ 邮箱白名单邮件推送飞书群机器人；Wails 托盘常驻，IMAP IDLE 监听、持久化水位线、结构化审计与本地控制面板。
status: implemented
manual_test: pending
created: "2026-11-14"
last_verified: ""
verified_scope: 单元测试、GUI 启动与真实 IMAP 连接、水位线持久化、飞书 Webhook 联调
manual_test_gap: 托盘菜单交互、审计面板真实数据、开机自启注册、重启续拉的人工验证
---

# mail2feishu

> **状态：桌面化功能已实现，尚未完成全部人工测试。**
> 已验证：`go test ./... -race`、GUI 启动并连接真实 QQ 邮箱、`state.json` 水位线落盘、飞书 Webhook 联调。
> 待人工验证：托盘菜单、配置页保存、审计面板、开机自启、重启续拉。
> 人工测试完成后，先询问用户是否更新此处状态与 `last_verified`。

QQ 邮箱白名单邮件推送飞书：常驻监听 IMAP，命中白名单的邮件经防抖聚合后推送飞书群自定义机器人。桌面版提供托盘与控制面板；Console 版保留脚本化能力。

设计文档：
- <../../docs/projects/go_projects/mail2feishu/plans/01-mail2feishu.md>
- <../../docs/projects/go_projects/mail2feishu/plans/02-mail2feishu-desktop.md>

## 能力表

| 业务用例 | GUI 入口 | CLI 入口 | 输入、结果与副作用 | 暴露或不暴露 MCP 的理由 |
| :--- | :--- | :--- | :--- | :--- |
| 常驻监听推送 | 双击 `mail2feishu-gui.exe` | 无参数运行 `mail2feishu-cli.exe` | 启动完整服务：IMAP 监听、白名单、防抖、飞书投递、审计 | 业务面是常驻监听，没有适合无状态工具调用的管理动作 |
| 控制面板 | 托盘「打开控制面板」 | 无（本地 HTTP API） | 查看状态/审计/日志，编辑配置并保存 | GUI 与 `127.0.0.1` 本地 API 已覆盖机器调用 |
| 配置校验 | 保存时校验 | `-check` | 校验配置语法与必填项；零网络 I/O | 部署前自检 |
| 测试投递 | 托盘/概览按钮 | `-test-webhook` | 发送一张测试卡片 | 一次性连通性验证 |
| 版本查询 | 托盘提示/面板 | `-version` | 输出版本、构建模式与 commit | 部署排查 |
| 单实例保护 | 自动 | 自动 | GUI 与 CLI 共用命名互斥，禁止同时监听造成重复推送 | 运行期安全约束 |

> 按《CLI 工具开发标准》§0.1，不提供 MCP 的理由已登记：本工具是常驻监听器，`-check` / `-test-webhook` / `-version` 与本地 HTTP API 已覆盖自动化需求；未实现 `schema` 导出。

## 白名单语义

- **默认全阻断**：白名单为空时所有邮件静默丢弃；From 无法解析时同样阻断。
- **规则之间 OR**；规则内部类别（邮箱/域名/关键词）之间 AND，同一类别内多个值 OR。
- **域名按标签边界匹配**：`github.com` 命中 `mail.github.com`，不命中 `evilgithub.com`。
- **配置写错即报错**：未知字段、空规则、非法邮箱/域名都会让保存/启动失败。

## 数据文件

| 文件 | 位置 | 说明 |
| :--- | :--- | :--- |
| `config.yaml` | 数据目录 | 配置（含凭证），权限 0600，严禁入库 |
| `state.json` | 数据目录 | UID 水位线（账号 + UIDVALIDITY + UID），原子写 |
| `audit.jsonl` | 数据目录 | 结构化审计，10 MiB 轮转保留 3 份 |
| `mail2feishu.log` | 数据目录 | 运行日志 |
| `webview2/` | 数据目录 | WebView2 用户数据，避免缓存散落 |

审计事件只含元信息（时间、UID、发件人、主题、动作、规则/原因、线程、推送耗时），不记录正文与凭证。`mail` 事件覆盖放行与丢弃，`push` 事件记录投递结果。

数据目录（按构建形态隔离）：

- dev：`%APPDATA%\language_projects\mail2feishu\dev\`
- release：`%APPDATA%\language_projects\mail2feishu\prod\`
- 取不到 `APPDATA` 时回退 `~/.language_projects/mail2feishu/<dev|prod>/`

## 运行

```powershell
# 桌面版：启动完整服务 + 托盘
.\bin\mail2feishu-gui-dev.exe

# 静默启动（只留托盘，不开主窗口）
.\bin\mail2feishu-gui-dev.exe --hidden
cscript //nologo .\run_background.vbs

# Console 版：前台常驻，Ctrl+C 退出
.\bin\mail2feishu-cli-dev.exe

# 配置校验 / 测试投递 / 版本
.\bin\mail2feishu-cli-dev.exe -check
.\bin\mail2feishu-cli-dev.exe -test-webhook
.\bin\mail2feishu-cli-dev.exe -version
```

GUI 与 CLI 共用同一数据目录和单实例锁，**同一时间只能运行一个监听实例**。托盘菜单提供：打开控制面板、测试飞书投递、立即重连、开机自启（仅 release）、彻底退出。

## 配置

首次启动 GUI 时若没有 `config.yaml`，界面仍会打开，在「配置」页填写并保存即可创建。凭证字段留空表示不修改已保存的值。

- `imap.username` / `imap.password`：QQ 邮箱地址与 **IMAP 授权码**（不是登录密码）。
- `feishu.webhook_url`：飞书群自定义机器人 Webhook；开启签名校验时填 `feishu.secret`。
- `ratelimit.debounce_window` / `thread_cooldown`：同讨论串防抖窗口与推送后冷却。
- 通过 GUI 保存会重写整个 YAML 文件，原有注释不保留；需要保留注释时请直接编辑文件。

## 链路自测

`scripts/send_test_mail.py` 用于验证「IMAP 收信 → 白名单 → 飞书推送」全链路，
凭证直接取自 `config.yaml`，脚本不保存密钥：

```powershell
uv run scripts/send_test_mail.py                     # 用配置里的账号给自己发一封自测邮件
uv run scripts/send_test_mail.py --verify            # 查看 INBOX 最新邮件与 UID
uv run scripts/send_test_mail.py --find m2f-test     # 在所有文件夹按主题查找
uv run scripts/send_test_mail.py --deliver m2f-test  # 把匹配邮件复制进 INBOX（等价于新邮件到达）
uv run scripts/send_test_mail.py --cleanup m2f-test --apply   # 清理自测邮件
```

> QQ 会把「自己发给自己」的邮件分流到自定义文件夹（如 `其他文件夹/测试目录`），
> 而本工具只监听 `INBOX`，所以自发邮件必须用 `--deliver` 注入 INBOX 才会触发推送。
> 更贴近真实场景的验证方式是从其它邮箱发信，或触发一次真实的 GitHub 通知。

## 构建

```powershell
uv run scripts/build.py              # dev：bin/mail2feishu-gui-dev.exe + bin/mail2feishu-cli-dev.exe
uv run scripts/build.py --release    # release：bin/mail2feishu-gui.exe + bin/mail2feishu-cli.exe
```

- release 仅在 HEAD 精确命中 `mail2feishu/v*` tag 且本项目路径工作区干净时允许。
- release 图标由 `scripts/render_icon.py` 生成绿色信封图标；dev 使用地鼠图标便于区分。
- 构建前用 go-winres 注入 VERSIONINFO + manifest + 图标到 `cmd/mail2feishu/rsrc_windows_amd64.syso`。

## 实现结构

```
cmd/mail2feishu/main.go     GUI 通道入口（!cli）：Wails + 托盘
cmd/mail2feishu/cli.go      Console 通道入口（cli）：常驻与运维命令
internal/app                Wails 壳、托盘、单实例、开机自启、路径与日志
internal/runtime            服务编排：配置、监听器、Pipeline、状态、重连
internal/api                本地 HTTP API 与统一响应包络
internal/web                内嵌 WebUI（原生 HTML/CSS/JS，无前端构建链）
internal/config             配置加载、默认值、严格校验与原子保存
internal/state              UID 水位线持久化
internal/audit              结构化审计（JSONL，轮转）
internal/matcher            零信任白名单匹配
internal/parser             MIME 解析与 GitHub 通知字段抽取
internal/feishu             飞书卡片 2.0 构建与 Webhook 投递
internal/ratelimit          按讨论串的防抖与冷却
internal/imap               IMAP IDLE 监听、水位线、断线指数退避重连
internal/service            业务编排：解析 → 过滤 → 防抖 → 投递
internal/buildinfo          ldflags 注入的版本信息
```

## 已验证 / 未验证

已验证（`go test ./... -race`、`go vet`、真实启动）：

- 白名单语义、MIME/GBK 解析、GitHub 字段抽取；
- 飞书卡片结构、签名、重试分类、业务错误码；
- 防抖聚合、冷却丢弃、并发安全；
- 审计事件（放行/丢弃/解析失败/推送成功或失败）；
- 水位线持久化（Save/Load、账号与 UIDVALIDITY 匹配、损坏回退、原子写）；
- runtime 配置视图脱敏、配置默认值、审计倒序读取、日志尾部读取；
- HTTP API（状态、配置、记录、日志、meta、静态页面）与 GUI 启动/真实 IMAP 连接；
- dev 双通道构建与 PE 资源注入。

待人工验证：

- 托盘菜单（打开面板、测试投递、重连、退出）与窗口关闭行为；
- 配置页保存后监听重启、白名单即时生效；
- 审计面板展示真实邮件记录；
- release 版开机自启注册与 `--hidden` 静默启动；
- 进程重启后从 `state.json` 续拉停机期间邮件。
