# 配套 copy_clictl_to_local_bin.ps1：从用户 PATH 目录移除 clictl.exe
$dest = "C:\Users\29580\.local\bin\clictl.exe"
if (Test-Path -LiteralPath $dest) {
    try {
        Remove-Item -LiteralPath $dest -ErrorAction Stop
        Write-Host "已删除: $dest"
    } catch {
        Write-Error "删除失败（clictl 可能正在运行）: $_"
        exit 1
    }
} else {
    Write-Host "未找到: $dest（无需删除）"
}
