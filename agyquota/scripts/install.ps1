# 将 agyquota.exe 部署到用户 PATH 目录（~/.local/bin 或 AppData/Local/agy/bin）
$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$exePath = Join-Path $repoRoot "agyquota.exe"

if (-not (Test-Path $exePath)) {
    Write-Host "未找到 agyquota.exe，先执行构建..."
    & (Join-Path $PSScriptRoot "build.ps1")
}

$destDirs = @(
    "C:\Users\29580\.local\bin",
    "C:\Users\29580\AppData\Local\agy\bin"
)

$targetDir = $destDirs | Where-Object { Test-Path $_ } | Select-Object -First 1

if (-not $targetDir) {
    throw "未找到有效的目标安装目录"
}

Copy-Item -LiteralPath $exePath -Destination $targetDir -Force
Write-Host "已将 agyquota.exe 安装到: $targetDir\agyquota.exe"
