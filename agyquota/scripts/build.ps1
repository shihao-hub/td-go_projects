# 构建单文件 agyquota.exe；版本与 buildID 均由 git describe 注入。
# 用法: ./scripts/build.ps1 [-Release]
#   -Release: 仅允许干净 tag 检出（describe 形如 v1.2.3 或 agyquota/v1.2.3）时构建
param(
    [switch]$Release
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    $describe = (& git describe --tags --always --dirty).Trim()
    if ($LASTEXITCODE -ne 0) { throw "git describe 失败" }

    if ($Release -and ($describe -match '-dirty' -or $describe -notmatch '^(?:agyquota/)?v\d+\.\d+\.\d+$')) {
        throw "-Release 要求干净 tag 检出（当前 describe: $describe）"
    }

    # buildID = describe + Unix 秒时间戳：区分 dirty 状态下的多次构建（v2 标准 §3.3）
    $buildID = "$describe.$([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())"

    Write-Host "正在构建 agyquota.exe ($describe, buildID=$buildID)..."
    go build -trimpath `
        -ldflags "-s -w -X agyquota/internal/buildinfo.Version=$describe -X agyquota/internal/buildinfo.BuildID=$buildID" `
        -o agyquota.exe ./cmd/agyquota
    if ($LASTEXITCODE -ne 0) { throw "go build 失败" }
    Write-Host "构建完成: $repoRoot\agyquota.exe ($describe)"
} finally {
    Pop-Location
}
