# TaskbarGuard

Windows 任务栏应用质感图标守卫与自动恢复引擎。

解决 Electron / InnoSetup 应用（如 VS Code、DeepSeek Harness 等）在后台自动静默更新后，PE 图标资源被官方默认图标覆盖导致任务栏视觉撕裂的痛点。

---

## 项目结构

```text
go_projects/taskbarguard/
├── go.mod                      # Go 模块文件 (go 1.25)
├── main.go                     # Go CLI 调度引擎
├── README.md                   # 项目总览
├── TASK_SCHEDULER.md           # Windows 计划任务完整管理手册 + 当前活跃任务清单
└── scripts/                    # 应用图标脚本资产库
    ├── README.md               # 脚本库全量款式、生成工序揭秘与手动运行速查手册
    ├── deepseek_harness/       # DeepSeek Harness 模块（款式 8 环形底光通透款已设为首选 ⭐）
    ├── idea/                   # IntelliJ IDEA 模块（经典微光彩极款 ⭐ / 极客银白 / 冰蓝科技）
    ├── vscode/                 # VS Code 模块（科技蓝微光 / 极客银白）
    └── wt/                     # Windows Terminal 模块（环形底光通透款 ⭐ / 钛金极客白 / 科技蓝）
```

---

## 配置持久化

CLI 默认在当前工作目录读写 `config.json`（该路径相对工作目录解析，请在项目根目录执行命令）。仓库内的 `config.json` 已纳入版本管理，作为偏好快照随仓库分发；当前生效偏好：

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

字段说明：`enabled` 控制 `apply` 是否批量执行；`style` 为脚本支持的款式名；`custom_icon` 为自定义 ICO 路径，设置它会替代 `style`。仅当该文件缺失时（如在仓库外首次运行）才会按上表自动生成。

```powershell
# 查看当前配置
go run . config
# 修改预设款式（选项可以写在 APP 前后）
go run . set vscode --style silver
# 指定自定义 ICO
go run . set deepseek_harness --custom-icon scripts/deepseek_harness/custom_icons/custom.ico
# 一键应用指定应用，或应用所有 enabled 应用（--kill 强制关闭占用进程）
go run . apply vscode
go run . apply --kill
# run 未指定 --style / --custom-icon 时自动回退配置
go run . run vscode
```

配置文件可通过 `--config PATH`（`config` 命令使用 `--file PATH`）切换。

---

## Go CLI 调度器

在项目根目录执行：

```powershell
# 列出可调度的应用与款式
go run . list

# 输出机器可读结果
go run . list --json

# 输出完整 CLI 契约，或指定子命令的契约
go run . schema
go run . list --schema
go run . run --schema

# 调度 VS Code 补丁脚本
go run . run --style blue vscode

# 调度 DeepSeek Harness 的自定义图标款式
go run . run --style 8_underlit_chrome deepseek_harness

# 调度 Windows Terminal (别名 wt / terminal；--kill 强制关闭占用进程)
go run . run --kill --style pure_dark wt
go run . run --style dark_blue wt
```

`run` 只负责发现脚本、校验参数并调度 `uv run`；不会在 Go 进程内执行 Python 逻辑。`install-task` 仅在 Windows 下注册登录触发的计划任务（执行 `apply`），实际脚本仍由守卫命令调用；开发版 exe 注册时会给出警告，正式启用请使用 release 版。

## 应用自动更新后的恢复

VS Code、DeepSeek Harness 等应用后台静默更新后会覆盖 PE 图标资源，导致任务栏视觉回退。确认更新完成后，在项目根目录执行：

```powershell
# 推荐：强制关闭占用进程并批量恢复所有已启用应用
.\taskbarguard-dev.exe apply --kill

# 或先手动完全退出目标应用，再执行
.\taskbarguard-dev.exe apply
```

注意：

- 必须在 `go_projects\taskbarguard` 目录下执行（`config.json` 与 `scripts\` 均按当前工作目录解析）；
- `idea` 当前在配置中禁用，`apply` 会自动跳过；需要时用 `go run . set idea --style underlit_chrome` 重新启用；
- 也可以直接用 Go 运行：`go run . apply --kill`。

## 双轨构建

`build.py` 使用 Go 标准构建流程，并注入 `main.version` 与 `main.commitHash`：

```powershell
# dirty / 开发分支也允许，输出 taskbarguard-dev.exe
python build.py --dev

# 仅允许干净工作区且 HEAD 精确对齐 tag，输出 taskbarguard.exe
python build.py --release
```

不带参数时依次尝试构建开发版与 Release 版；Release 条件不满足会明确失败。开发版版本号带 `-dev` 或 `-dirty` 后缀，正式版使用当前 tag（例如 `v1.0.0` 注入为 `1.0.0`）。

---

## 快速手动尝试

详细参数、对比图与工序解析请阅读 [scripts/README.md](scripts/README.md)。

### DeepSeek Harness
```powershell
# 【款式 8 · 当前首选】应用环形底光立体款（上下通透）
uv run scripts/deepseek_harness/patch_deepseek_harness_icon.py --custom-icon scripts/deepseek_harness/custom_icons/8_underlit_chrome.ico --kill

# 恢复官方默认
uv run scripts/deepseek_harness/patch_deepseek_harness_icon.py --restore
```

### IntelliJ IDEA
```powershell
# 【强烈推荐 🏆】应用环形底光通透立雕款（与 DeepSeek 8号同源底光技术，体量饱满，对齐 Zed）
uv run scripts/idea/patch_idea_icon.py --style underlit_chrome --kill

# 【极简立雕】应用纯粹微光立体徽标款
uv run scripts/idea/patch_idea_icon.py --style pure_sculpted --kill

# 【高光光刃】应用钛金高对比光刃款
uv run scripts/idea/patch_idea_icon.py --style titanium_prism --kill

# 恢复官方默认
uv run scripts/idea/patch_idea_icon.py --restore
```

### VS Code
```powershell
# 切换为 科技蓝微光款（推荐）
uv run scripts/vscode/patch_vscode_icon.py --style blue --kill

# 切换为 极客银白款
uv run scripts/vscode/patch_vscode_icon.py --style silver --kill

# 恢复官方默认
uv run scripts/vscode/patch_vscode_icon.py --restore
```

---

## 文档导航

- [scripts/ 资产库使用指南 (含工序解析、全量款式清单与一键命令)](scripts/README.md)
- [Windows 计划任务查看与管理手册](TASK_SCHEDULER.md)
