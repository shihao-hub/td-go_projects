# liteconf 构建脚本：构建 server 二进制到 <项目根>/build/
# 用法：
#   .\scripts\build.ps1            # 增量构建
#   .\scripts\build.ps1 -Clean     # 清空 build/ 后全量构建
param(
    [switch]$Clean
)

$ErrorActionPreference = "Stop"

# 项目根 = 本脚本所在 scripts/ 的上一级，不依赖调用位置
$projectRoot = Split-Path -Parent $PSScriptRoot
$buildDir = Join-Path $projectRoot "build"
$output = Join-Path $buildDir "liteconf-server.exe"

if ($Clean -and (Test-Path $buildDir)) {
    Remove-Item -Recurse -Force $buildDir
    "cleaned: $buildDir"
}

if (-not (Test-Path $buildDir)) {
    New-Item -ItemType Directory -Path $buildDir | Out-Null
}

# -trimpath 去除本机路径；-s -w 裁剪符号表与调试信息减小体积
Push-Location $projectRoot
try {
    go build -trimpath -ldflags "-s -w" -o $output ./cmd/liteconf-server
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed (exit $LASTEXITCODE)"
    }
}
finally {
    Pop-Location
}

if (-not (Test-Path $output)) {
    throw "build finished but output not found: $output"
}

$sizeMB = "{0:N2}" -f ((Get-Item $output).Length / 1MB)
"build ok: $output ($sizeMB MB)"
