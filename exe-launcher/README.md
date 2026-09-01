# exe-launcher

EXE 启动器：把常用的 exe 集中管理，一键启动 / 打开目录 / 开 PowerShell。
纯 Win32 API（Go + syscall），单文件零依赖。

功能与使用说明见 [docs/exe-launcher.md](docs/exe-launcher.md)。

## 目录结构

```
├── cmd/exe-launcher/        # 入口（薄 main + .syso 资源）
├── internal/
│   ├── win32/               # Win32 syscall 基座：proc 表 / 常量 / 结构体 / 小工具
│   ├── model/               # 数据模型：Entry / Store / Config / 系统标签（纯逻辑 + 落盘）
│   ├── scan/                # 目录扫描：递归收集 *.exe，跳过噪音目录
│   ├── mdview/              # Markdown → RTF 纯函数转换
│   └── ui/                  # 窗口 / 托盘 / 对话框 / 文件选择框 / 单例 / 日志
├── winres/                  # .syso 的源
│   ├── icon.png             #   256x256 源图标（make-icon.ps1 生成）
│   ├── make-icon.ps1        #   重画图标用
│   └── winres.json          #   图标组 / 清单 / 版本信息定义
├── scripts/
│   └── build.ps1            # 构建脚本，产物输出到仓库根
└── docs/                    # 文档与静态页面
    ├── exe-launcher.md      # 功能说明
    └── demo.html
```

`rsrc_windows_amd64.syso` 由 winres 生成，必须与主包（`cmd/exe-launcher`）同目录，
`go build` 会自动链接它。

## 构建

GUI 程序，构建时必须加 `-H windowsgui`，否则启动会弹一个终端黑窗：

```powershell
go build -trimpath -ldflags "-s -w -H windowsgui" -o exe-launcher.exe ./cmd/exe-launcher
```

- `-H windowsgui`：把 PE 子系统设为 Windows GUI，不创建控制台窗口。
  代价是没有 stdout/stderr，`fmt.Println` 无处输出，日志一律走
  `%AppData%\exe-launcher\exe-launcher.log`（见 logging.go）
- `-s -w`：去掉符号表和 DWARF 调试信息，减小体积
- `-trimpath`：去除二进制里的本机路径

等价做法（推荐）：

```powershell
.\scripts\build.ps1
```

### 改图标 / 资源

改 `winres\icon.png` 或 `winres\winres.json` 后重新生成 .syso 再构建（在仓库根执行）：

```powershell
go-winres make --arch amd64 --out cmd\exe-launcher\rsrc
.\scripts\build.ps1
```

## 测试

```powershell
go test ./...
```

## 常见问题

**Q：exe 在资源管理器里不显示图标，启动一次后才显示？**

图标已通过 `.syso` 嵌入 exe，这不是构建问题，是 Explorer 的图标缓存
（按文件路径索引）没有随重新构建失效。刷新缓存即可：

```powershell
ie4uinit.exe -show
```

顽固时删除 `%LOCALAPPDATA%\Microsoft\Windows\Explorer\iconcache_*.db`
并重启资源管理器，或临时改一下 exe 文件名。

窗口/任务栏图标正常是另一条路径：运行期由程序自己 `LoadIconW` 从 exe
资源加载再 `WM_SETICON`（window.go），与 Explorer 文件图标缓存无关。
