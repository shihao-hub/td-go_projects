---
name: mail2feishu
description: QQ 邮箱白名单邮件推送飞书群机器人；IMAP IDLE 常驻监听，命中白名单后推送交互式卡片。
status: implemented
manual_test: pending
created: "2026-11-14"
last_verified: ""
verified_scope: 单元测试（go test ./...）、真实 IMAP 登录、SMTP 自发信、飞书 Webhook 推送联调
manual_test_gap: 真实 GitHub 通知、长时间常驻与断线重连
---

# mail2feishu

> **状态：功能已实现，尚未人工测试。**
> 已验证：单元测试、真实 IMAP 登录、SMTP 自发信、飞书 Webhook 推送联调。
> 未验证：真实 GitHub 通知触发、长时间常驻与断线重连恢复。
> 人工测试完成后，先询问用户是否更新此处状态与 `last_verified`。

QQ 邮箱白名单邮件推送飞书：常驻监听 IMAP，命中白名单的邮件经防抖聚合后推送飞书群自定义机器人。

## 背景

QQ 邮箱接收 GitHub Issue/PR、VIP 等邮件，但手机不希望开启全局邮箱通知。本工具在电脑端常驻监听 `imap.qq.com:993`，采用**零信任白名单**：未命中规则的邮件完全静默，命中后推送飞书交互式卡片（GitHub 邮件特化展示仓库、编号、触发人与直达按钮）。

设计文档：<../../docs/projects/go_projects/mail2feishu/plans/01-mail2feishu.md>

## 能力表

| 业务用例 | CLI 入口 | MCP 工具 | 输入、结果与副作用 | 暴露或不暴露的理由 |
| :--- | :--- | :--- | :--- | :--- |
| 常驻监听推送 | 无参数启动 | 不暴露 | 加载配置后连接 IMAP，命中白名单的邮件推送飞书；无持久化业务状态 | 本工具是常驻守护进程，业务面是「监听 → 推送」，没有适合无状态工具调用的管理动作 |
| 配置校验 | `-check` | 不暴露 | 校验配置语法与必填项；零网络 I/O | 部署前自检，供人工与脚本使用 |
| 测试投递 | `-test-webhook` | 不暴露 | 向配置的 Webhook 发送一张测试卡片 | 一次性连通性验证，副作用是群内出现一条测试消息 |
| 版本查询 | `-version` | 不暴露 | 输出版本、构建模式与 commit | 部署排查 |

> 按《CLI 工具开发标准》§0.1，不提供 MCP 的理由已在此登记：本工具没有「跨请求管理状态」的业务面，也没有查询/修改类命令；`-check` / `-test-webhook` / `-version` 已覆盖机器可调的运维动作。按 §5.5 对不提供 MCP 的独立 CLI 的例外，本项目未实现 `schema` 导出。

## 白名单语义

- **默认全阻断**：白名单为空时所有邮件静默丢弃；From 无法解析时同样阻断。
- **规则之间 OR**：命中任意一条规则即放行。
- **规则内部类别 AND / 类别内 OR**：一条规则里配置的 `sender_emails`、`sender_domains`、`subject_keywords` 三类条件都必须满足（未配置的类别不约束）；同一类别内多个值命中任意一个即可。
- **域名后缀按标签边界匹配**：`github.com` 命中 `mail.github.com`，不命中 `evilgithub.com`。
- **配置写错即报错**：未知字段、空规则、非法邮箱/域名都会让进程拒绝启动，避免白名单静默失效。

## 配置

复制 `config.yaml.example` 为 `config.yaml` 并填写：

- `imap.username` / `imap.password`：QQ 邮箱地址与**IMAP 授权码**（不是 QQ 登录密码）。
- `feishu.webhook_url`：飞书群自定义机器人 Webhook；开启签名校验时填 `feishu.secret`。
- `ratelimit.debounce_window`：同一讨论串的防抖窗口（默认 15s）。
- `ratelimit.thread_cooldown`：同一讨论串推送后的冷却时间（默认 10m）。

`config.yaml` 含凭证，已在 `.gitignore` 中，严禁入库。

配置文件查找顺序：`-config` 显式路径 > 默认数据目录下的 `config.yaml`。

## 运行

```powershell
# 前台运行（Ctrl+C 退出）
.\bin\mail2feishu-dev.exe -config .\config.yaml

# 后台静默运行
cscript //nologo .\run_background.vbs
```

`run_background.vbs` 优先启动 `bin\mail2feishu.exe`，不存在时回退 `bin\mail2feishu-dev.exe`。

数据目录（按构建形态隔离）：

- dev：`%APPDATA%\language_projects\mail2feishu\dev\`
- release：`%APPDATA%\language_projects\mail2feishu\prod\`
- 取不到 `APPDATA` 时回退 `~/.language_projects/mail2feishu/<dev|prod>/`

日志写入数据目录下的 `mail2feishu.log`，同时输出到 stderr。

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
uv run scripts/build.py              # dev 构建 -> bin/mail2feishu-dev.exe
uv run scripts/build.py --release    # release 构建 -> bin/mail2feishu.exe
```

release 仅在 HEAD 精确命中 `mail2feishu/v*` tag 且本项目路径工作区干净时允许；版本、模式与 commit 通过 `-ldflags` 注入。构建前用 `rsrc` 把 `icon.ico` 写入 `rsrc_windows_amd64.syso`。

## 实现结构

```
main.go                     组装入口：flag、配置、日志、信号
internal/config             配置加载、默认值、严格校验
internal/matcher            零信任白名单匹配
internal/parser             MIME 解析与 GitHub 通知字段抽取
internal/feishu             飞书卡片 2.0 构建与 Webhook 投递（签名/重试）
internal/ratelimit          按讨论串的防抖与冷却
internal/imap               IMAP IDLE 监听、断线指数退避重连
internal/service            业务编排：解析 → 过滤 → 防抖 → 投递
internal/buildinfo          ldflags 注入的版本信息
```

## 已验证 / 未验证

已验证（`go test ./... -race`）：

- 白名单匹配语义（精确邮箱、域名后缀、关键词、AND/OR、默认阻断、畸形 From）；
- MIME 解析（multipart/alternative、quoted-printable、GBK、HTML-only、GitHub 尾部清理）；
- GitHub 字段抽取（List-Id / Subject / URL 回退、Issue 与 PR 区分）；
- 飞书卡片结构、签名、重试分类、业务错误码；
- 防抖聚合、冷却丢弃、并发安全；
- 业务编排（非白名单不推送、GitHub 卡片内容、同讨论串聚合）；
- `build.py` dev 构建与 `-check` / `-test-webhook`（后者用本地 mock Webhook 验证过 HTTP 流程）。

尚未验证（需要真实凭证）：

- 真实 QQ 邮箱 IMAP 登录与 IDLE 新邮件事件；
- 真实飞书群 Webhook 投递（配置后运行 `-test-webhook` 即可验证）。
