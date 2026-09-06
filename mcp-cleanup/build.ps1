# 构建 mcp-cleanup.exe（控制台程序，内嵌 PowerShell 脚本，可从任意目录执行）
$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
Push-Location $root
go build -trimpath -ldflags "-s -w" -o mcp-cleanup.exe .
Pop-Location
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Output "built: $(Join-Path $root 'mcp-cleanup.exe')"
