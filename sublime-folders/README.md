# sublime-folders

托盘常驻工具，定时记录 Sublime Text 打开的目录到 SQLite。数据与日志：`%APPDATA%\sublime-folders\`。

## 构建

```powershell
.\scripts\build-tray.ps1      # 托盘版（GUI）：build\sublime-folders.exe，-H windowsgui 隐藏控制台
.\scripts\build-practice.ps1  # 练习版（CLI）：build\sublime-folders-practice.exe，保留控制台输出
```

两个脚本均可从任意目录执行，产物固定在项目 `build\` 目录。
