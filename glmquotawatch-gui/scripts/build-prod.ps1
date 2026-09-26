<#
.SYNOPSIS
    编译生产版 glmquotawatch-gui.exe
.DESCRIPTION
    产物：bin/glmquotawatch-gui.exe
    环境：默认单例互斥锁、数据目录指向 %APPDATA%\language_projects\glmquotawatch-gui\prod，允许开机自启
#>
param(
    [switch]$SkipFrontend
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
    Write-Host "=== [1/3] 检查并构建前端资源 ===" -ForegroundColor Cyan
    if (-not $SkipFrontend) {
        Push-Location "frontend"
        try {
            # 如果 dist 目录存在，先清理旧构建产物，防止 Windows 下 Vite/Rolldown 覆盖文件时偶发 EBUSY
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
        wails3 generate syso -arch amd64 -icon windows/icon.ico -manifest windows/wails.exe.manifest -info windows/info.json -out ../wails_windows_amd64.syso
        if ($LASTEXITCODE -ne 0) { throw "wails3 generate syso 失败" }
    } finally {
        Pop-Location
    }

    $outExe = Join-Path $binDir "glmquotawatch-gui.exe"
    Write-Host "=== [3/3] 编译生产版可执行文件 -> $outExe ===" -ForegroundColor Cyan

    $version = "0.1.0"
    $ldflags = "-w -s -H windowsgui -X glmquotawatch-gui/internal/cli.Version=$version"

    go build -tags production -trimpath -buildvcs=false -ldflags $ldflags -o $outExe .
    if ($LASTEXITCODE -ne 0) { throw "go build 失败" }

    Write-Host "✓ 生产版构建成功: $outExe" -ForegroundColor Green
    Get-Item $outExe | Select-Object Name, Length, LastWriteTime
} finally {
    if (Test-Path "$projectRoot\*.syso") {
        Remove-Item "$projectRoot\*.syso" -Force -ErrorAction SilentlyContinue
    }
    Pop-Location
}