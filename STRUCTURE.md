# Go 多入口项目结构说明（sublime-folders）

> 本文记录本项目 2026-09 从"单目录单入口"重构为"cmd 多入口 + 共享包"的完整做法。
> 后续新建 Go 项目时，可让 AI **开发之初就按此结构组织代码**，避免重构。

## 一、最终结构

```
sublime-folders/
├── go.mod                        module 声明（module sublime-folders），全项目唯一
├── session.go                    ┐
├── store.go                      │ 共享代码包（package app）
├── capture.go                    │ 位置在模块根目录，import 路径就是模块名
├── show.go                       │ 如 "sublime-folders"
├── tray.go                       │
├── log.go                        │
├── alert_windows.go / alert_other.go   平台差异用构建标签 //go:build windows / !windows
├── mutex_windows.go / mutex_other.go
├── assets/                       静态资源（embed 用，必须与引用它的 .go 同目录层级）
├── cmd/                          ★ 入口目录：每个子目录 = 一个独立可执行程序
│   ├── sublime-folders/main.go   托盘版（正式工具），package main
│   └── practice/main.go          练习版（CLI），package main，自包含
├── scripts/
│   ├── build-tray.ps1            构建 → build\sublime-folders.exe（-H windowsgui 隐藏控制台）
│   └── build-practice.ps1        构建 → build\sublime-folders-practice.exe（保留控制台）
└── build/                        构建产物（*.exe 已 gitignore）
```

## 二、背后的 Go 硬规则（为什么这样设计）

1. **一个目录 = 一个 package**，`package main` 全目录只允许一个 `func main()`。
   → 想要多个入口，每个入口必须独占一个目录，这就是 `cmd/<名字>/` 的由来。
2. **`package main` 不能被 import**。
   → 共享代码绝不能放在 `package main` 里，必须放在普通包（本项目是根目录的 `app` 包）。
3. **跨包调用只能用大写字母开头的名字（导出）**。
   → 小写 = 包内私有。共享包里"预计会被入口调用"的函数/类型必须首字母大写。

## 三、重构时做了什么（清单）

1. 根目录 9 个文件 `package main` → `package app`，仅包名与导出改名，**文件位置不动**：
   - `loadAutoSession→LoadAutoSession`、`currentFolders→CurrentFolders`、
     `sessionData/sessionWindow/sessionFolder→SessionData/SessionWindow/SessionFolder`
   - `store→Store`、`openStore→OpenStore`、`dataDir→DataDir`
   - `captureLoop→CaptureLoop`、`runTray→RunTray`
   - `alertError→AlertError`、`acquireSingleInstance→AcquireSingleInstance`
   - `initLogging` 从入口文件移入共享包成为 `InitLogging`（日志逻辑两个入口都可能要）
   - 纯内部函数（showText/showCurrent/captureOnce/pruneOnce、Store 的查询方法等）保持小写
2. 原单文件 `main.go`（练习版）与 `main_by_ai.go`（托盘版）各自成为 `cmd/` 下的真入口；
   托盘版入口调用共享包：`import app "sublime-folders"`，然后 `app.OpenStore()` 等。
3. 构建 `build.ps1` 拆为 `scripts/build-*.ps1` 两个，用 `$PSScriptRoot` 定位仓库根，
   **从任意目录执行都不会跑偏**；产物统一输出到 `build/`。

## 四、日常命令

| 场景 | 命令 |
|---|---|
| 跑练习版 | `go run ./cmd/practice` |
| 本地跑托盘版（调试） | `go run ./cmd/sublime-folders -no-tray` |
| 跑托盘版（带托盘） | `go run ./cmd/sublime-folders` |
| 构建托盘版 exe | `.\scripts\build-tray.ps1` |
| 构建练习版 exe | `.\scripts\build-practice.ps1` |
| 全量检查 | `gofmt -l .` + `go vet ./...` + `go build ./...` |

练习版想调用现成实现：`import app "sublime-folders"` 后直接 `app.LoadAutoSession()` 等。

## 五、新增第三个入口的步骤（模板）

1. `mkdir cmd/<新名字>`，写 `main.go`（`package main` + `func main()`）
2. 复用共享包：`import app "sublime-folders"`
3. 需要新命令行参数就用 `flag`，需要新共享逻辑就加到根目录 `app` 包并导出
4. 需要独立构建就加 `scripts/build-<新名字>.ps1`（GUI 程序记得 `-H windowsgui`）

## 六、给未来项目的初始结构模板

新建 Go 项目时直接让 AI 按下面骨架开工，事后无需重构：

```
<项目名>/
├── go.mod                  # module <项目名>
├── *.go                    # 共享代码，package 用短名（如 app），不用 main
├── assets/                 # embed 资源，跟引用它的 .go 放一起
├── cmd/<入口A>/main.go     # 每个入口一个子目录，package main
├── cmd/<入口B>/main.go
├── scripts/build-*.ps1     # 一个入口一个脚本，$PSScriptRoot 定位根目录
└── build/                  # 产物目录，配合 .gitignore 的 *.exe
```

要点：**共享代码从第一天就放普通包并规划好导出名；入口只做参数解析和流程编排**。
