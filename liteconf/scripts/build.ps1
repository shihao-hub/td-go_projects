# liteconf 构建脚本：构建 server 与调试 CLI 两个二进制到 <项目根>/build/
# 用法：
#   .\scripts\build.ps1                  # 增量构建（版本号默认 0.1.0）
#   .\scripts\build.ps1 -Clean           # 清空 build/ 后全量构建
#   .\scripts\build.ps1 -Version 0.2.0   # 指定版本号，注入两个二进制
param(
    [switch]$Clean,
    [string]$Version = "0.1.0"
)

$ErrorActionPreference = "Stop"

# 项目根 = 本脚本所在 scripts/ 的上一级，不依赖调用位置
$projectRoot = Split-Path -Parent $PSScriptRoot
$buildDir = Join-Path $projectRoot "build"
$serverOutput = Join-Path $buildDir "liteconf-server.exe"
$cliOutput = Join-Path $buildDir "liteconf.exe"

if ($Clean -and (Test-Path $buildDir)) {
    Remove-Item -Recurse -Force $buildDir
    "cleaned: $buildDir"
}

if (-not (Test-Path $buildDir)) {
    New-Item -ItemType Directory -Path $buildDir | Out-Null
}

# 版本号经 internal/version.Version 注入，两个二进制使用同一条 ldflags（注入方式一致）
$ldflags = "-s -w -X github.com/shihao-hub/liteconf/internal/version.Version=$Version"

# -trimpath 去除本机路径；-s -w 裁剪符号表与调试信息减小体积
Push-Location $projectRoot
try {
    go build -trimpath -ldflags $ldflags -o $serverOutput ./cmd/liteconf-server
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed (exit $LASTEXITCODE)"
    }
    go build -trimpath -ldflags $ldflags -o $cliOutput ./cmd/liteconf
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed (exit $LASTEXITCODE)"
    }
}
finally {
    Pop-Location
}

foreach ($output in @($serverOutput, $cliOutput)) {
    if (-not (Test-Path $output)) {
        throw "build finished but output not found: $output"
    }
}

$serverSizeMB = "{0:N2}" -f ((Get-Item $serverOutput).Length / 1MB)
$cliSizeMB = "{0:N2}" -f ((Get-Item $cliOutput).Length / 1MB)
"build ok: $serverOutput ($serverSizeMB MB)"
"build ok: $cliOutput ($cliSizeMB MB)"
