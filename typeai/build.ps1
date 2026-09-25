param(
    [string]$Version = "dev"
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$outputDir = Join-Path $projectRoot "build"
New-Item -ItemType Directory -Force -Path $outputDir | Out-Null

Push-Location $projectRoot
try {
    go build -trimpath -ldflags "-s -w -X typeai/internal/cli.Version=$Version" -o (Join-Path $outputDir "typeai.exe") ./cmd/typeai
} finally {
    Pop-Location
}

Write-Host "built: $(Join-Path $outputDir 'typeai.exe')"
