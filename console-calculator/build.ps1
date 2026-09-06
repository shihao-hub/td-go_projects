# 构建 console-calculator.exe（控制台程序，可从任意目录执行）
$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
Push-Location $root
go build -trimpath -ldflags "-s -w" -o console-calculator.exe .
Pop-Location
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Output "built: $(Join-Path $root 'console-calculator.exe')"
