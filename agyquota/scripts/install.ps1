# 将 agyquota.exe 部署到用户 PATH 目录
$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$exePath = Join-Path $repoRoot "agyquota.exe"

if (-not (Test-Path $exePath)) {
    Write-Host "未找到 agyquota.exe，先执行构建..."
    & (Join-Path $PSScriptRoot "build.ps1")
}

$destDirs = @(
    "D:\Users\language_projects_bin",
    "C:\Users\29580\.local\bin",
    "C:\Users\29580\AppData\Local\agy\bin"
)

$installed = $false
foreach ($dir in $destDirs) {
    if (Test-Path $dir) {
        Copy-Item -LiteralPath $exePath -Destination $dir -Force
        Write-Host "已将 agyquota.exe 安装到: $dir\agyquota.exe"
        $installed = $true
    }
}

if (-not $installed) {
    throw "未找到有效的目标安装目录"
}
