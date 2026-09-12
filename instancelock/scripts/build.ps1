# instancelock 构建脚本：注入版本号并生成 SHA256SUMS
# 用法: ./scripts/build.ps1 [-Version X.Y.Z]（默认 dev）
param([string]$Version = "dev")

$ErrorActionPreference = "Stop"
Set-Location -LiteralPath (Join-Path $PSScriptRoot "..")
New-Item -ItemType Directory -Force -Path build | Out-Null

$out = "build\instancelock-windows-amd64.exe"
go build -ldflags "-X main.version=$Version" -o $out .\cmd\instancelock
if ($LASTEXITCODE -ne 0) { throw "go build 失败" }

$hash = (Get-FileHash -Algorithm SHA256 $out).Hash.ToLower()
"$hash  $(Split-Path -Leaf $out)" | Set-Content -Encoding ascii -NoNewline:$false build\SHA256SUMS.txt
Write-Host "$out (version=$Version) 构建完成，SHA256SUMS.txt 已生成"
