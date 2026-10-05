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
    └── vscode/                 # VS Code 模块（科技蓝微光 / 极客银白）
```

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
```

`run` 只负责发现脚本、校验参数并调度 `uv run`；不会在 Go 进程内执行 Python 逻辑。`install-task` 仅在 Windows 下注册登录触发的计划任务，实际脚本仍由守卫命令调用。

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
