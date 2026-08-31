# exe-launcher

**EXE 启动器**：把常用的 exe 集中管理，一键启动 / 打开目录 / 开 PowerShell。
纯 Win32 API 实现（Go + syscall），单文件零依赖。

## 功能

- 手动添加 exe，或扫描目录批量导入（勾选确认，不自动入库）
- 一键启动（ShellExecute，支持 UAC 提权程序）、打开所在目录、在目录开 PowerShell
- 失效条目标红、一键清理；状态栏实时统计
- **单实例**：重复启动会提示并自动激活已运行的窗口
- **查看介绍**：为 exe 配套同目录同名 `.md`（如 `foo.exe` → `foo.md`），工具栏或右键「介绍」即弹出渲染后的 Markdown 介绍窗口
- 右键菜单与工具栏共用同一套命令

## 数据

配置与日志都落在 `%AppData%\exe-launcher\`：

- `config.json` 条目列表（名称 / 路径 / 添加时间）
- `exe-launcher.log` 运行日志（panic / 关键动作全记录）

## 给 exe 写介绍

在 exe 同目录放一个同名 md 即可。支持标题、粗体、斜体、行内代码、
代码块、列表、引用、分隔线和链接。
