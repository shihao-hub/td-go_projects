$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
go build -trimpath -ldflags "-s -w -H windowsgui" -o exe-launcher.exe ./cmd/exe-launcher
Pop-Location
