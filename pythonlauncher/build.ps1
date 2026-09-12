$ErrorActionPreference = "Stop"

go build -trimpath -ldflags "-s -w" -o (Join-Path $PSScriptRoot "launcher.exe") .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Output "built: $(Join-Path $PSScriptRoot 'launcher.exe')"
