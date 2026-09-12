# instancelock 一键发布脚本：构建 -> tag -> push -> GitHub Release
# 用法: ./scripts/release.ps1 -Version 1.0.0 [-Notes "说明"]
# 前提: gh CLI 已登录；工作区干净（instancelock/ 内无未提交变更）
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Notes = ""
)

$ErrorActionPreference = "Stop"
Set-Location -LiteralPath (Join-Path $PSScriptRoot "..")

# 1. semver 校验
if ($Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "版本号需为 X.Y.Z 形式，实际: $Version"
}
$tag = "instancelock/v$Version"

# 2. tag 重复检查
git rev-parse -q --verify "refs/tags/$tag" | Out-Null
if ($LASTEXITCODE -eq 0) {
    throw "tag $tag 已存在"
}

# 3. 工作区干净检查（仅限本项目目录）
$dirty = git status --porcelain -- instancelock
if ($dirty) {
    throw "instancelock/ 存在未提交变更，请先提交:`n$dirty"
}

# 4. 构建（注入版本号 + 生成 SHA256SUMS）
& (Join-Path $PSScriptRoot "build.ps1") -Version $Version

# 5. 打 tag 并推送
git tag $tag
git push origin $tag

# 6. 创建 GitHub Release 并上传附件
if ($Notes -eq "") { $Notes = "instancelock v$Version" }
gh release create $tag build\instancelock-windows-amd64.exe build\SHA256SUMS.txt `
    --title "instancelock v$Version" --notes $Notes

Write-Host "发布完成: $tag"
Write-Host "验证: gh release view $tag"
