# copy_launcher

交互式部署工具：把 `pythonlauncher\launcher.exe` 复制到指定目录，按提示选名字（[1] 目标目录名 / [2] 自定义 / [3] launcher.exe；目标已存在时询问覆盖）。

## 用法

```powershell
# 在 pythonlauncher 目录下（先跑 .\build.ps1 产出 launcher.exe）
go -C .\scripts\copy_launcher\ run . D:\tools\zedhub
```

exe 默认带站标地鼠图标：本目录下的 `icon.ico` 与 `rsrc_windows_amd64.syso` 由 `go build` 自动链接，无需额外参数；更换图标时用 rsrc 重出 syso（见父仓 `docs/go_projects/SKILL-GO-EXE-ICON.md`）。

## ⚠️ 定位机制：编译期烤入的源码路径（重要）

`projectRoot()` 用 `runtime.Caller(0)` 拿 `main.go` 的**源码绝对路径**，三次上溯到项目根后拼 `launcher.exe`。

反直觉但必须知道的三件事：

1. **路径是编译时写死进二进制的**（Go 把每个源文件的绝对路径烤进行号表 pclntab，供 panic 堆栈和 `runtime.Caller` 读取）。所以把 `copy_launcher.exe` 拷到任何地方运行，它"知道"的仍是构建机上那份源码的路径——不是运行时去找源码，是字符串快照。
2. **找不到就干净报错**：源码目录被移动/删除，或 exe 拷到没有这套源码的机器上 → `os.Stat` 失败 → stderr 输出 `launcher.exe not found: ... (run .\build.ps1 first)`，exit 1，不崩溃不误拷。
3. **禁止用 `-trimpath` 构建**：会把烤入路径变成模块相对路径（`pythonlauncher/scripts/copy_launcher/main.go`），上溯逻辑直接失效。`go run` 和默认 `go build` 都没问题。

为什么不用 `os.Executable()` / cwd：`go run` 下 Executable 指向临时构建目录，cwd 随调用方式变化（`go -C` 时是脚本目录），源码路径是唯一稳定锚点。

## 退出码

| 码 | 含义 |
| --- | --- |
| 0 | 复制成功 |
| 1 | 运行错误（launcher.exe 缺失 / 目标目录不存在 / 覆盖取消等） |
| 2 | 用法错误（参数个数不对） |

注：经 `go run` 调用时非零退出码会被 go 工具折叠为 1。
