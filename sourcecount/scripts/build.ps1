param(
    [string]$Version = "dev"
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$buildDir = Join-Path $projectRoot "build"
New-Item -ItemType Directory -Force -Path $buildDir | Out-Null

$ldflags = "-X sourcecount/internal/cli.Version=$Version -X sourcecount/internal/mcp.Version=$Version"
go build -ldflags $ldflags -o (Join-Path $buildDir "sourcecount.exe") ./cmd/sourcecount
Write-Host "已生成 $buildDir\sourcecount.exe ($Version)"