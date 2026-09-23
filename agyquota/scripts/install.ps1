# 将 agyquota.exe 部署到用户 PATH 目录
$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$exePath = Join-Path $repoRoot "agyquota.exe"

if (-not (Test-Path $exePath)) {
    Write-Host "未找到 agyquota.exe，先执行构建..."
    & (Join-Path $PSScriptRoot "build.ps1")
}

$targetDir = "D:\Users\language_projects_bin"
if (-not (Test-Path $targetDir)) {
    throw "未找到目标安装目录: $targetDir"
}

Copy-Item -LiteralPath $exePath -Destination $targetDir -Force
Write-Host "已将 agyquota.exe 安装到: $targetDir\agyquota.exe"
