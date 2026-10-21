# TaskbarGuard

Windows 任务栏应用质感图标守卫与自动恢复引擎。

解决 Electron / InnoSetup 应用（如 VS Code、DeepSeek Harness 等）在后台自动静默更新后，PE 图标资源被官方默认图标覆盖导致任务栏视觉撕裂的痛点。

---

## 双通道产物

| 通道 | 产物 | 用途 |
| :--- | :--- | :--- |
| GUI | `taskbarguard-gui.exe` / `taskbarguard-gui-dev.exe` | 双击零黑框，托盘常驻 + 桌面工作台；**不接受任何子命令** |
| Console | `taskbarguard-cli.exe` / `taskbarguard-cli-dev.exe` | 终端 / 脚本 / Agent / 计划任务专用，shell 正常等待，管道可用 |

> 终端里请始终使用 `-cli` 产物。GUI 产物是 `windowsgui` 子系统，PowerShell 不会等待它，`--json` 与重定向都不会有输出。

---

## 项目结构

```text
go_projects/taskbarguard/
├── go.mod
├── cmd/taskbarguard/
│   ├── main.go                 # GUI 入口（!cli）
│   └── cli.go                  # Console 入口（cli）
├── internal/
│   ├── core/                   # service：发现 / 补丁 / 回退 / 配置 / 计划任务
│   ├── api/                    # GUI 与 cli serve 共用的 HTTP Handler
│   ├── web/                    # go:embed 原生 WebUI
│   ├── app/                    # Wails 壳、托盘、窗口几何、日志、自启
│   └── buildinfo/              # 版本与 dev/prod 通道
├── scripts/                    # 图标补丁资产库 + build.py / render_icon.py
├── winres/                     # PE VERSIONINFO 模板
├── info.json / wails.exe.manifest
└── TASK_SCHEDULER.md
```

---

## 配置与数据目录

配置与运行时数据统一放在：

```text
%APPDATA%\language_projects\taskbarguard\<dev|prod>\config.json
```

- dev 产物写 `dev`，release 产物写 `prod`，互不覆盖；
- GUI 与 CLI 共用同一通道配置；
- 首次运行会把 exe 同级（或当前工作目录）的历史 `config.json` 复制到新目录，旧文件保留；
- `scripts/` 默认按 exe 同级目录解析（仓库内开发时即项目根），可用 `--scripts-dir` 覆盖；
- `--config` / `--file` 仍可显式指定配置路径。

仓库根目录的 `config.json` 作为默认偏好快照随仓库分发，当前内容：

```json
{
  "apps": {
    "deepseek_harness": { "enabled": true, "style": "8_underlit_chrome" },
    "idea": { "enabled": false, "style": "underlit_chrome" },
    "vscode": { "enabled": true, "style": "silver" },
    "wt": { "enabled": true, "style": "pure_dark" }
  }
}
```

字段说明：`enabled` 控制 `apply` 是否批量执行；`style` 为脚本支持的款式名；`custom_icon` 为自定义 ICO 路径，设置它会替代 `style`。

---

## 桌面工作台

```powershell
# 打开窗口（默认动态端口）
.\taskbarguard-gui-dev.exe

# 静默托盘（开机自启用）
.\taskbarguard-gui-dev.exe --hidden

# 不装 GUI 也能用浏览器打开同一套界面
.\taskbarguard-cli-dev.exe serve --open
```

工作台能力：

- 应用卡片列表，展示脚本路径、预设款式与启用状态；
- 切换款式 / 启用开关，即时写入配置；
- 单应用「应用」「恢复官方」；
- 批量应用全部已启用应用，可勾选强制关进程与重启 Explorer；
- 注册登录守护计划任务；
- 底部执行日志（uv 脚本输出已按 UTF-8 捕获）。

窗口尺寸、位置与最大化状态记录在数据目录的 `window.json`，关闭窗口后进程留在托盘，再次打开会恢复上次几何。

---

## 命令行

```powershell
# 列出可调度的应用与款式
.\taskbarguard-cli-dev.exe list

# 输出机器可读结果
.\taskbarguard-cli-dev.exe list --json

# 输出 CLI 契约
.\taskbarguard-cli-dev.exe schema
.\taskbarguard-cli-dev.exe run --schema

# 调度补丁
.\taskbarguard-cli-dev.exe run --style blue vscode
.\taskbarguard-cli-dev.exe run --style 8_underlit_chrome deepseek_harness
.\taskbarguard-cli-dev.exe run --kill --style pure_dark wt

# 恢复官方图标
.\taskbarguard-cli-dev.exe revert vscode

# 配置
.\taskbarguard-cli-dev.exe config
.\taskbarguard-cli-dev.exe set vscode --style silver
.\taskbarguard-cli-dev.exe set deepseek_harness --custom-icon scripts/deepseek_harness/custom_icons/8_underlit_chrome.ico

# 按配置批量应用（--kill 强制关闭占用进程）
.\taskbarguard-cli-dev.exe apply vscode
.\taskbarguard-cli-dev.exe apply --kill

# 内嵌 WebUI
.\taskbarguard-cli-dev.exe serve --port 0 --open

# 注册登录守护计划任务（指向同目录的 taskbarguard-cli）
.\taskbarguard-cli-dev.exe install-task
```

`run` 只负责发现脚本、校验参数并调度 `uv run`；不会在 Go 进程内执行 Python 逻辑。

---

## 应用自动更新后的恢复

VS Code、DeepSeek Harness 等应用后台静默更新后会覆盖 PE 图标资源。确认更新完成后执行：

```powershell
# 推荐：强制关闭占用进程并批量恢复所有已启用应用
.\taskbarguard-cli-dev.exe apply --kill

# 或先手动完全退出目标应用
.\taskbarguard-cli-dev.exe apply
```

**Windows Terminal 独立版（wt）**：免安装版不带自动更新器。升级请用幂等命令 —— 已是最新则退出 0，失败自动回滚：

```powershell
uv run scripts\wt\patch_wt_icon.py --update
```

注意：`--auto-setup` 只在安装目录为空时联网部署，**在已装机上不会升级**。

---

## 构建

```powershell
# dev：taskbarguard-gui-dev.exe + taskbarguard-cli-dev.exe
uv run scripts/build.py

# release：taskbarguard-gui.exe + taskbarguard-cli.exe
# 需 HEAD 精确命中 taskbarguard/v* tag 且工作区干净，否则退出码 2
git tag taskbarguard/v1.0.0
uv run scripts/build.py --release
```

- 版本注入 `taskbarguard/internal/buildinfo.Version`，通道注入 `BuildMode`；
- release 使用 `scripts/render_icon.py` 生成的盾牌图标，dev 使用 Go 地鼠；
- PE 资源由 `go-winres` 注入（VERSIONINFO + manifest + icon），GUI 与 CLI 的 FileDescription 各自独立。

---

## 快速手动尝试

详细参数、对比图与工序解析请阅读 [scripts/README.md](scripts/README.md)。

### DeepSeek Harness
```powershell
uv run scripts/deepseek_harness/patch_deepseek_harness_icon.py --custom-icon scripts/deepseek_harness/custom_icons/8_underlit_chrome.ico --kill
uv run scripts/deepseek_harness/patch_deepseek_harness_icon.py --restore
```

### IntelliJ IDEA
```powershell
uv run scripts/idea/patch_idea_icon.py --style underlit_chrome --kill
uv run scripts/idea/patch_idea_icon.py --style pure_sculpted --kill
uv run scripts/idea/patch_idea_icon.py --restore
```

### VS Code
```powershell
uv run scripts/vscode/patch_vscode_icon.py --style blue --kill
uv run scripts/vscode/patch_vscode_icon.py --style silver --kill
uv run scripts/vscode/patch_vscode_icon.py --restore
```

---

## 文档导航

- [scripts/ 资产库使用指南 (含工序解析、全量款式清单与一键命令)](scripts/README.md)
- [Windows 计划任务查看与管理手册](TASK_SCHEDULER.md)
