# 构建练习版（CLI，保留控制台输出）
# 用法: .\scripts\build-practice.ps1   （可从任意目录执行）
$root = Split-Path -Parent $PSScriptRoot
go build -trimpath -o (Join-Path $root "build\sublime-folders-practice.exe") (Join-Path $root "cmd\practice")
