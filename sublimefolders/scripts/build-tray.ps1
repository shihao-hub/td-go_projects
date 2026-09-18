# 构建托盘版（GUI，隐藏控制台窗口）
# 用法: .\scripts\build-tray.ps1   （可从任意目录执行）
$root = Split-Path -Parent $PSScriptRoot
New-Item -ItemType Directory -Force -Path (Join-Path $root "build") | Out-Null
go build -trimpath -ldflags "-s -w -H windowsgui" -o (Join-Path $root "build\sublimefolders.exe") (Join-Path $root "cmd\sublimefolders")
