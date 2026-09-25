# glmquotawatch-gui

智谱 GLM（bigmodel）编码套餐用量监控的**桌面 GUI 工具**（Wails v3 + 托盘常驻）：定时采样各额度窗口用量，跨越阈值档位（默认 50/60/80/90%）时弹原生 Windows Toast 告警；主窗仪表盘 + 历史趋势曲线；内置 60 倍速演示模式。归档项目 `glmquotawatch`（CLI 版）的 GUI 复活版。

- **GUI**（面向人）：无参数启动；关窗最小化到托盘，监控持续
- **CLI**（面向机器/AI）：恒输出 JSON 信封，无人读模式；`schema` 导出 CLI 自身契约（`interface: "cli"`）
- **不含 MCP server**（用户决策；偏离《CLI 工具开发标准》默认双入口，理由：GUI 面向人 + CLI 恒 JSON 已覆盖机器消费，替代入口即本 CLI）

## 构建

前置：Go 1.25+、Node.js 20+、wails3 CLI（`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.25`）。

```powershell
cd go_projects\glmquotawatch-gui
npm install          # 首次，在 frontend/ 内（wails3 build 也会按需执行）
wails3 build         # 产出 bin\glmquotawatch-gui.exe（含版本号注入）
wails3 dev           # 开发模式（前端热更新）
```

- exe 图标：`build/windows/icon.ico`（仓库站标地鼠，与项目根 `icon.ico` 同源）经 `wails3 generate syso` 注入；wails 构建后自动清理根目录临时 `*.syso`，**不要提交静态 syso**。
- 纯 `go build .`：仅在 `frontend/dist` 已存在时可用（先跑过一次前端构建）。
- 版本号：`build/config.yml` 的 `info.version` 与 `build/windows/Taskfile.yml` 的 `-X …cli.Version` 保持同步。

## GUI

| 入口 | 说明 |
|---|---|
| 无参数启动 | 主窗 + 托盘；关窗到托盘，退出仅经托盘菜单 |
| `--demo` / 托盘「进入演示模式」 | 60 倍速模拟：5 分钟走完 5 小时窗口 0→100%，依次触发各档 Toast；写独立演示目录，不污染真实数据；可随时退出恢复 |
| `--hidden` | 静默启动（开机自启注册形态）：不弹主窗，仅托盘 |
| 托盘菜单 | 显示主窗 / 立即采样 / 静音 / 开机自启（HKCU Run 键，登录后 `--hidden` 启动）/ 演示模式 / 退出 |
| 重复启动 | 单实例：激活既有主窗后新进程退出 |

设置页配置 token（≥ 20 字符）、interval（30s–24h）、thresholds（1-99）、hysteresis（0-30）、silent；历史页看 24h/7d 百分比曲线（含阈值虚线）。

## CLI（恒 JSON）

```powershell
.\bin\glmquotawatch-gui.exe token set <你的GLM_API_TOKEN>   # 配置后 GUI/CLI 共用
.\bin\glmquotawatch-gui.exe status                          # 立即采样一次
.\bin\glmquotawatch-gui.exe schema                          # CLI 契约目录（零 I/O）
```

- 全部命令 stdout 恒输出 `{"ok":true,"data":…}` / `{"ok":false,"error":{"code","message"}}`（`--help`/`--version` 亦信封化）；退出码 0 成功 / 1 业务失败 / 2 参数错误。
- 子命令：`token set/show/remove`、`status`、`config show/set`、`schema`、`version`。
- `--json` flag 已移除（恒 JSON）——传入会按未知 flag 报 `bad_args` 退出码 2。

## 数据目录

`%APPDATA%\language_projects\glmquotawatch-gui\`：`config.json`（token 明文本机保存）、`state.json`（已告警档位记录）、`samples-YYYY-MM.jsonl`（采样历史）；`demo\` 子目录为演示数据（每次进入演示清空重建）。环境变量 `GLMQUOTAWATCH_GUI_API_BASE` 可覆盖上游地址（默认 `https://open.bigmodel.cn`）。

## Wails beta 升级必回归项

1. 关窗→托盘→再显示链路（`RegisterHook(WindowClosing)+Cancel`）
2. 单实例：第二实例退出时机与 `--demo` 转发
3. 通知静音实机效果（`NotificationSound{Silent:true}`）
4. `--hidden` 静默启动不闪窗

## 已知限制

- 通知以应用自身 AUMID 发出；若系统通知设置关闭则静默丢弃（排查路径同 Windows 通知设置）。
- dev 期（`go run` / 临时构建产物）拒绝注册开机自启（错误 `dev_executable`），请用 `wails3 build` 产物验证。
- TIME_LIMIT（MCP 月度额度）告警、突发消耗检测、z.ai 国际版端点留二期。
