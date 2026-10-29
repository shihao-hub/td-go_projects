# NutVault (坚果云轻量文件版本备份时光机)

基于 **Go + Wails v3** 遵循 [`sh-cmd-desktop-dual-channel`](file:///D:/Users/language_projects/.agents/skills/sh-user-skills/sh-desktop-dual-channel/README.md) 桌面双通道规范打造。

---

## 一、核心特性

1. **扁平化路径映射与多版本管理**：
   - 自动将 Windows 本地绝对路径转义为坚果云规范目录名：  
     例如：`C:\Users\29580\AppData\Roaming\Zed\settings.json`  
     $\rightarrow$ `C:\Users\29580\Nutstore\1\我的坚果云\C-Users-29580-AppData-Roaming-Zed\settings-20261026225218.json`
   - 时间戳版本快照（保留 stem + `YYYYMMDDHHmmss` + ext），若同秒内高频触发自动递增防覆盖序号。
2. **安全恢复时光机 (Safe Restore)**：
   - 支持将坚果云快照直接覆盖恢复回源文件；
   - 恢复前自动原地创建带有时间戳的安全备份副本（`*.pre_restore.bak`），杜绝手抖误覆盖。
3. **精准 Hash 去重与流量保护 (Traffic Guard)**：
   - **智能 SHA256 比对**：源文件内容未改动时自动跳过备份（`skipped_unchanged`），坚果云零同步流量；
   - **体积门禁防护**：支持配置单文件最大大小限制（默认 20MB），防止误加大文件消耗坚果云每月流量配额；
   - **流量仪表盘**：实时统计当月累计上传/下载流量。
4. **后台定时巡检 (Scheduler)**：
   - 轻量后台调度器，按设定周期（如每 60 分钟）自动轮询所有受保护文件；
   - 若文件无变动则直接跳过，零网络与磁盘写入负担。
5. **高可用 WebUI 时光机工作台**：
   - 内置响应式暗色主题工作台；
   - 实时路径映射计算器预览；
   - 行级别 Diff 差异查看器（在恢复前清晰比对修改内容）；
   - 一键直达坚果云目录与本地源目录。
6. **双通道五维隔离**：
   - GUI（`-H windowsgui`）带系统托盘、`window.json` 几何记忆、关窗销毁 WebView2 释放 300MB+ 内存；
   - CLI 纯控制台子系统，支持纯脚本自动化。

---

## 二、快速开始

### 1. 编译构建

```powershell
# 开发构建 (生成 bin/nutvault-gui-dev.exe 与 bin/nutvault-cli-dev.exe)
uv run scripts/build.py

# 发布构建 (生成 bin/nutvault-gui.exe 与 bin/nutvault-cli.exe)
uv run scripts/build.py --release
```

### 2. 命令行 (CLI) 快速用法

```powershell
# 登记受保护文件
.\bin\nutvault-cli-dev.exe add "C:\Users\29580\AppData\Roaming\Zed\settings.json"

# 查看当前规则列表
.\bin\nutvault-cli-dev.exe list

# 触发全量增量检查备份 (文件未修改会自动跳过)
.\bin\nutvault-cli-dev.exe backup --all

# 查看某文件的历史快照
.\bin\nutvault-cli-dev.exe snapshots 1

# 比对历史快照与当前本地文件的 Diff 差异
.\bin\nutvault-cli-dev.exe diff 1

# 安全恢复某快照 (会自动生成 *.pre_restore.bak)
.\bin\nutvault-cli-dev.exe restore 1

# 启动 WebUI 服务 (在浏览器中打开)
.\bin\nutvault-cli-dev.exe serve --port 8765
```

### 3. 桌面 GUI 客户端

双击运行 `bin/nutvault-gui.exe` 或 `bin/nutvault-gui-dev.exe`：
- 托盘图标常驻，关闭窗口自动释放内存（降至 ~25MB）；
- 点击托盘图标或菜单唤醒工作台，窗口位置与尺寸平滑恢复；
- 点击右上角「设置」可配置坚果云同步根目录、流量上限与定时检查频率。
