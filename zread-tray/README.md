# zread-tray

系统托盘常驻管理 zread browse，功能与用法详见 [zread-tray.md](zread-tray.md)。

## 构建

```powershell
.\build.ps1    # 等价于：go build -trimpath -ldflags "-s -w -H windowsgui" -o zread-tray.exe .
```

GUI 托盘程序，`-H windowsgui` 隐藏控制台窗口，产物在项目根，可从任意目录执行脚本。
