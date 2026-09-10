# Human SPEC

请参考下面的设计完成需求，请发挥主观能动性，下面的设计不一定对！

## 需求背景

将 python-launcher-go 目录的 launcher.exe 复制到指定目录下

## 需求分析

1. 定位到 launcher.exe 所在位置
2. 复制 exe 到指定目录下

## 需求设计


```go
func locateLauncherExe() (string, error)

func copyLauncherExe(dst string) error
```


# AI SPEC

## 用法

```powershell
# 在 python-launcher-go 目录下，目标目录为任意已存在目录
go -C .\scripts\copy_launcher\ run . D:\tools\zedhub
# 交互选择名字后 → 复制 launcher.exe 到 D:\tools\zedhub\<所选名字>.exe
```

## 设计（已确认）

1. 参数：唯一参数 `<目标目录>`，直接使用该路径（不再拼接兄弟项目、不校验 `pyproject.toml`）；缺参 → usage 到 stderr，exit 2
2. 定位 launcher.exe：`runtime.Caller(0)` 取 main.go 源码绝对路径，三次上溯到项目根后拼 `launcher.exe`（`go run` 下 CWD 不可靠，源码路径是唯一稳定锚点）；缺失 → 提示先跑 `.\build.ps1`，exit 1
3. 交互改名，三选一（非法输入循环重问）：
   - `[1]` 目标目录名（`D:\tools\zedhub` → `zedhub.exe`）
   - `[2]` 自定义输入（空输入重问；无 `.exe` 后缀自动补）
   - `[3]` 默认名 `launcher.exe`
4. 目标文件已存在 → `overwrite? [y/N]` 确认，非 `y` 取消 exit 1
5. 复制实现：读源文件 + 写目标（保留权限位）；成功输出 `copied: <dst>`
6. 约定：纯标准库；错误统一 stderr 前缀 `copy_launcher:`；退出码 0 成功 / 1 运行错误 / 2 用法错误；经 `go run` 调用时非零退出码会被 go 工具折叠为 1

## 函数

```go
func projectRoot() (string, error)      // 源码锚点定位项目根
func locateLauncherExe() (string, error)
func copyLauncherExe(dst string) error  // dst 为完整目标文件路径
func askName(stdin *bufio.Reader, targetDir string) (string, error)
```
