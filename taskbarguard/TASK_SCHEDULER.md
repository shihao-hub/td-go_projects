# Windows 计划任务（Task Scheduler）查看与管理手册

本文档记录 Windows 计划任务的完整管理与排查方法，并附当前系统中的自定义活跃任务清单，供 `taskbarguard` 项目后续实现开机自愈守护参考。

---

## 一、 图形界面管理（GUI · 最直观）

1. **快速打开**：
   - 按键盘快捷键 `Win + R`；
   - 输入 `taskschd.msc` 并回车。
2. **定位自建任务**：
   - 在左侧导航栏展开 **“任务计划程序库 (Task Scheduler Library)”**；
   - 大多数第三方软件或用户自建的常驻任务都会直接位于该根目录下（而非深层的 `Microsoft\Windows` 系统目录下）。
3. **常用 GUI 操作**：
   - **运行 (Run)**：右键任务 -> 立即手动测试触发一次；
   - **禁用 (Disable)**：暂停该任务执行，但保留配置；
   - **属性 (Properties)**：查看或修改**触发器 (Triggers)**（如开机时、特定时间、空闲时）和**操作 (Actions)**（启动程序路径、启动参数）。

---

## 二、 命令行与 PowerShell 管理（自动化与脚本友好）

PowerShell 提供了非常强大的内置模块 `ScheduledTasks`：

### 1. 查询任务
```powershell
# 1. 查看位于根目录下（通常为第三方/用户自定义）的所有非禁用任务
Get-ScheduledTask | Where-Object { $_.TaskPath -eq "\" -and $_.State -ne "Disabled" } | Format-Table TaskName, State

# 2. 按名称模糊搜索
Get-ScheduledTask -TaskName "*notify*"

# 3. 查看某个任务的具体执行命令与参数
(Get-ScheduledTask -TaskName "todonotify_morning").Actions

# 4. 查看某个任务的触发条件 (Triggers)
(Get-ScheduledTask -TaskName "todonotify_morning").Triggers
```

### 2. 状态控制与删除
```powershell
# 禁用任务
Disable-ScheduledTask -TaskName "任务名称"

# 启用任务
Enable-ScheduledTask -TaskName "任务名称"

# 立即手动触发
Start-ScheduledTask -TaskName "任务名称"

# 删除任务（无需确认）
Unregister-ScheduledTask -TaskName "任务名称" -Confirm:$false
```

### 3. 经典 CMD 命令（兼容批处理）
```cmd
:: 列出所有任务的表格视图
schtasks /query /fo TABLE

:: 查看指定任务的详细参数与上次运行结果
schtasks /query /tn "任务名称" /v /fo LIST

:: 强制删除任务
schtasks /delete /tn "任务名称" /f
```

---

## 三、 本机当前非系统活跃计划任务清单（截至 2026-10-05）

经扫描，当前系统根路径（`\`）下运行的非 Windows 默认内置任务如下：

| 任务名称 (TaskName) | 当前状态 | 业务推断 / 归属组件 | 说明 |
| :--- | :--- | :--- | :--- |
| **`Clash Verge`** | `Running` | 网络代理客户端 | 随系统启动守护代理服务 |
| **`douyinnotify_check`** | `Ready` | 本机自动化/抖音相关提醒 | 定时检查与提醒脚本 |
| **`todonotify_morning`** | `Ready` | 个人效率日程工作流 | 早间待办事项推送通知 |
| **`todonotify_evening`** | `Ready` | 个人效率日程工作流 | 晚间待办事项复盘推送 |
| **`AMDInstallLauncher`** | `Ready` | AMD 芯片组/显卡驱动 | 驱动更新安装引导器 |
| **`NVIDIA App SelfUpdate_...`** | `Ready` | NVIDIA App 显卡驱动 | 官方后台自更新检查 |
| **`NahimicTask32` / `NahimicTask64`** | `Running` | 硬件音频驱动音效套件 | 伴随系统常驻服务 |
| **`CreateExplorerShellUnelevatedTask`** | `Ready` | 系统资源管理器挂载辅助 | 负责非提权外壳唤起 |
| **`User_Feed_Synchronization_...`** | `Ready` | RSS/系统源同步组件 | 系统底层订阅源同步 |

---

## 四、 TaskbarGuard 守护设计要点与硬核准则

当后续在 `taskbarguard` 中通过 Go 创建 `TaskbarIconGuard` 计划任务时，必须严格遵守以下准则：

1. **触发时机推荐**：
   - 采用 **“用户登录时 (AtLogon)”** 触发；
   - 用户开机登录时，系统剛完成初始化，此时各开发工具（VS Code / DeepSeek）的官方更新已完成落盘，在此处进行 Hash 检查并注入补丁最为干净无感。
2. **零黑框静默运行**：
   - 使用 Go 编译的控制台程序在任务计划静默运行时，应附加 `-H=windowsgui` 或通过 Windows API 隐藏控制台，避免开机弹黑框打扰用户。
3. **不可违背的铁律：绝不重启 Explorer**：
   - 计划任务运行在非交互式后台安全上下文中，绝对不允许执行 `taskkill /F /IM explorer.exe`（会导致用户交互 Token 丢失进而黑屏假死）；
   - 刷新外壳一律只调用 `os.utime()` 触碰快捷方式并调用 Win32 API `SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_FLUSH, 0, 0)`。
