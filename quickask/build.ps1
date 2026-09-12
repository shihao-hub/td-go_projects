$ErrorActionPreference = "Stop"

# fyne GUI 已弃：CLI 客户端 + 后端守护两个控制台程序
New-Item -ItemType Directory -Force -Path bin | Out-Null

go build -o bin\quickaskd.exe .\cmd\quickaskd
go build -o bin\quickask.exe .\cmd\quickask

if ($?) {
    Write-Host "build OK: bin\quickask.exe + bin\quickaskd.exe" -ForegroundColor Green
}
