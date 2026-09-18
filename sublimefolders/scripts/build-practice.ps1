# 构建练习版（CLI，保留控制台输出）
# 用法: .\scripts\build-practice.ps1   （可从任意目录执行）
$root = Split-Path -Parent $PSScriptRoot
New-Item -ItemType Directory -Force -Path (Join-Path $root "build") | Out-Null
go build -trimpath -o (Join-Path $root "build\sublimefolders-practice.exe") (Join-Path $root "cmd\practice")
