$ErrorActionPreference = "Stop"

$gcc = "C:\msys64\ucrt64\bin"
if (Test-Path $gcc) {
    $env:Path = "$gcc;$env:Path"
}

New-Item -ItemType Directory -Force -Path bin | Out-Null

go build -o bin\aiquickd.exe .\cmd\aiquickd
go build -ldflags "-H windowsgui -s -w" -o bin\aiquick.exe .\cmd\aiquick

if ($?) {
    Write-Host "build OK: bin\aiquick.exe + bin\aiquickd.exe" -ForegroundColor Green
}
