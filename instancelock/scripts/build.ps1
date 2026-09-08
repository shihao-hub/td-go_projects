$ErrorActionPreference = "Stop"
Set-Location -LiteralPath (Join-Path $PSScriptRoot "..")
New-Item -ItemType Directory -Force -Path build | Out-Null
go build -o build\instancelock.exe .\cmd\instancelock
Write-Host "build\instancelock.exe 构建完成"
