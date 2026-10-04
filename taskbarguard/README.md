# TaskbarGuard

Windows 任务栏应用质感图标守卫与自动恢复引擎。

解决 Electron / InnoSetup 应用（如 VS Code、DeepSeek Harness 等）在后台自动静默更新后，PE 图标资源被官方默认图标覆盖导致任务栏视觉撕裂的痛点。

---

## 项目结构

```text
go_projects/taskbarguard/
├── go.mod                      # Go 模块文件 (go 1.25)
├── main.go                     # Go CLI 调度引擎骨架（占位待实现）
├── README.md                   # 项目总览
├── TASK_SCHEDULER.md           # Windows 计划任务完整管理手册 + 当前活跃任务清单
└── scripts/                    # 应用图标脚本资产库
    ├── README.md               # 脚本库全量款式、生成工序揭秘与手动运行速查手册
    ├── deepseek_harness/       # DeepSeek Harness 模块（款式 5 钛金属立体款已设为首选）
    └── vscode/                 # VS Code 模块（科技蓝微光 / 极客银白）
```

---

## 快速手动尝试

详细参数、对比图与工序解析请阅读 [scripts/README.md](scripts/README.md)。

### DeepSeek Harness
```powershell
# 【款式 5 · 当前首选】应用钛金属立体雕刻款
uv run scripts/deepseek_harness/patch_deepseek_harness_icon.py --custom-icon scripts/deepseek_harness/custom_icons/5_titanium_sculpted.ico --kill

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
