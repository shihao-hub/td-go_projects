# 构建单文件 agyquota.exe；版本号通过 -ldflags -X 注入
# 用法: ./scripts/build.ps1 [-Version 0.2.0]，默认 0.2.0
param(
    [string]$Version = "0.2.0"
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    Write-Host "正在构建 agyquota.exe (v$Version)..."
    go build -trimpath -ldflags "-s -w -X agyquota/internal/cli.Version=$Version" -o agyquota.exe ./cmd/agyquota
    if ($LASTEXITCODE -ne 0) { throw "go build 失败" }
    Write-Host "构建完成: $repoRoot\agyquota.exe (v$Version)"
} finally {
    Pop-Location
}
