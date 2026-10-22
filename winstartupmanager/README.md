# WinStartupManager (Windows 开机自启可视化管理工具)

轻量透明、现代审美、无损启停、支持注册表深度管理的 Windows 开机自启动项可视化管理工具。

## 核心特性

- **注册表四大物理位置全纳管**：
  - `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
  - `HKCU\Software\Microsoft\Windows\CurrentVersion\RunOnce`
  - `HKLM\Software\Microsoft\Windows\CurrentVersion\Run`
  - `HKLM\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`
- **任务管理器底层同款无损启停**：
  - 对齐 Windows 任务管理器底层 `StartupApproved\Run` 与 `Run32` 二进制位翻转机制（12 字节二进制状态值）。
  - 禁用时不篡改或删除原命令行，随时一键恢复，与 Windows 任务管理器完全双向同步。
- **健康审计与数字签名**：
  - 自动探测目标执行体文件存在性，醒目标记“无效残留项”并支持一键清理。
  - 基于 Win32 原生 `version.dll` 提取发布商签名或厂商名称。
- **预删除安全快照防护**：
  - 在执行底层物理删除前，强制在 `%LOCALAPPDATA%\WinStartupManager\backups\` 自动落地该条目的标准 `.reg` 文本备份。
- **系统级联动**：
  - 一键在资源管理器中精准定位并高亮可执行文件 (`explorer.exe /select`)。
  - 一键在注册表编辑器中跳转并高亮目标项 (`regedit.exe` via `LastKey`)。
- **只读审计扩展看板**：
  - 启动文件夹（`%APPDATA%` 与 `%ProgramData%` Startup 快捷方式解析）。
  - 计划任务（只读扫描开机 Boot 与登录 Logon 触发的任务）。
- **极简低内存常驻架构（手记 071 实战标准）**：
  - Chromium 启动参数剪枝（砍掉独立 GPU 与网络进程）。
  - 关窗彻底销毁 WebView2，异步调用 GC 与 FreeOSMemory 归还系统堆内存。
  - 托盘双击毫秒级按需重建窗口；开机自启 `--hidden` 零 Chromium 纯 Go 实例运行（常驻物理内存 ~20MB）。
- **窗口几何记忆**：
  - 尺寸、位置、最大化状态落 `window.json`，移窗/缩放 700ms 防抖保存，关窗同步落盘。
  - 外接屏拔出后旧坐标不可见时自动拉回主屏居中。
- **通道隔离**：dev / release 使用不同数据目录、单实例 ID、图标（dev 为灰蓝 DEV 图标）与版本号。

## 构建与运行

### 依赖环境
- Go 1.23+
- Node.js 20+ / npm
- uv (Python 包与脚本运行工具)
- Wails v3 CLI (`wails3`)
- go-winres (`go install github.com/tc-hib/go-winres@latest`)

### 开发构建 (dev)
```bash
uv run build.py --dev
```
产物输出至 `build/winstartupmanager-dev.exe`（`-tags dev`，灰蓝 DEV 图标）。

### 正式打包 (release)
```bash
uv run build.py --release
```
产物输出至 `build/winstartupmanager.exe`；仅当 HEAD 精确命中 `winstartupmanager/v*` tag 且工作区干净时允许，否则退出码 2。

### 运行
```bash
# 默认打开主界面
./build/winstartupmanager-dev.exe

# 静默启动进系统托盘（开机自启形态）
./build/winstartupmanager-dev.exe --hidden
```

## 数据目录

- 运行时数据：`%APPDATA%\language_projects\winstartupmanager\{dev,prod}\`
  - `backups/`：删除前的 `.reg` 快照（旧版 `%LOCALAPPDATA%\WinStartupManager\backups` 会在首次访问时复制过来，源目录保留）
  - `webview2/`：WebView2 用户数据
  - `window.json`：窗口尺寸/位置/最大化状态
  - `winstartupmanager.log`：运行日志

## 生产排障（release 无控制台）

release 产物使用 `-H windowsgui`，没有控制台窗口，排障只依赖落盘日志：

- dev 与 release **都**双写 `stderr + <数据目录>/winstartupmanager.log`，dev 只是额外能看到控制台。
- 单文件上限 2 MB，超限后下次启动轮转为 `winstartupmanager.log.1`（只保留一份）。
- 启动横幅记录 `version / commit / mode`；Wails 内部日志（WebView2、AssetServer）与前端 JS 异常（`[FE]` 前缀，同消息 1 分钟去重）一并写入。
- 安全约定：日志不记录启动项命令行原文与注册表值内容，只记来源、名称与错误。
- release 启动失败会弹出对话框并给出日志路径。

```powershell
Get-Content -Encoding UTF8 -Tail 50 "$env:APPDATA\language_projects\winstartupmanager\prod\winstartupmanager.log"
```

