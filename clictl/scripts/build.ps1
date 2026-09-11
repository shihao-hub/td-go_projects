# 构建单文件 clictl.exe；版本号唯一来源 = -ldflags -X 注入
# 用法: ./scripts/build.ps1 [-Version 1.0.0]，默认 dev
param(
    [string]$Version = "dev"
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    go build -trimpath -ldflags "-s -w -X clictl/internal/cli.Version=$Version" -o clictl.exe ./cmd/clictl
    if ($LASTEXITCODE -ne 0) { throw "go build 失败" }
    Write-Host "构建完成: $repoRoot\clictl.exe (v$Version)"
} finally {
    Pop-Location
}
