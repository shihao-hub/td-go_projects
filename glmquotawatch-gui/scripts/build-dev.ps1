<#
.SYNOPSIS
    编译开发版 glmquotawatch-gui-dev.exe（注入 dev 模式、独立单例、独立数据目录）
.DESCRIPTION
    产物：bin/glmquotawatch-gui-dev.exe
    环境：Dev 单例互斥锁、数据目录指向 %APPDATA%\language_projects\glmquotawatch-gui\dev，禁止开机自启
    默认使用 -H windowsgui 隐藏控制台黑框；若需要调试日志可传 -ShowConsole
#>
param(
    [switch]$SkipFrontend,
    [switch]$ShowConsole
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
    Write-Host "=== [1/3] 检查并构建前端资源 ===" -ForegroundColor Cyan
    if (-not $SkipFrontend) {
        Push-Location "frontend"
        try {
            if (Test-Path "dist") {
                Remove-Item "dist" -Recurse -Force -ErrorAction SilentlyContinue
            }
            Write-Host "正在编译前端 (vite build --mode production)..."
            npm run build
            if ($LASTEXITCODE -ne 0) { throw "前端构建失败" }
        } finally {
            Pop-Location
        }
    } else {
        Write-Host "跳过前端构建 (-SkipFrontend)" -ForegroundColor Yellow
    }

    $binDir = Join-Path $projectRoot "bin"
    if (-not (Test-Path $binDir)) {
        New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    }

    Write-Host "=== [2/3] 生成 Windows 资源 (.syso) ===" -ForegroundColor Cyan
    Push-Location "build"
    try {
        wails3 generate syso -arch amd64 -icon windows/icon-dev.ico -manifest windows/wails.exe.manifest -info windows/info.json -out ../wails_windows_amd64.syso
        if ($LASTEXITCODE -ne 0) { throw "wails3 generate syso 失败" }
    } finally {
        Pop-Location
    }

    $outExe = Join-Path $binDir "glmquotawatch-gui-dev.exe"
    Write-Host "=== [3/3] 编译开发版可执行文件 -> $outExe ===" -ForegroundColor Cyan

    $version = "0.1.0-dev"
    $guiFlag = if ($ShowConsole) { "" } else { "-H windowsgui" }
    $ldflags = "$guiFlag -X glmquotawatch-gui/internal/cli.Version=$version".Trim()

    go build -tags dev -buildvcs=false -gcflags=all="-l" -ldflags "$ldflags" -o $outExe .
    if ($LASTEXITCODE -ne 0) { throw "go build 失败" }

    Write-Host "✓ 开发版构建成功: $outExe" -ForegroundColor Green
    Get-Item $outExe | Select-Object Name, Length, LastWriteTime
} finally {
    if (Test-Path "$projectRoot\*.syso") {
        Remove-Item "$projectRoot\*.syso" -Force -ErrorAction SilentlyContinue
    }
    Pop-Location
}