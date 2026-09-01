# make-icon.ps1 — 生成 winres\icon.png（256x256 源图标）
#
# 视觉：深靛蓝渐变圆角底 + 2x2 彩色圆角方块网格（应用启动器隐喻），
# 右下角白色方块内嵌靛蓝播放三角，点出「启动」。16px 托盘尺寸下四色网格仍可辨。
# 改完重新生成本图后，需再执行（在仓库根目录）：
#   go-winres make --arch amd64 --out cmd\exe-launcher\rsrc
# 把各尺寸（16/32/48/256）打包进 cmd\exe-launcher\rsrc_windows_amd64.syso 并重新 scripts\build.ps1。

Add-Type -AssemblyName System.Drawing

$size   = 256   # 画布边长
$radius = 58    # 外框圆角半径

$bmp = New-Object System.Drawing.Bitmap($size, $size)
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias

function New-RoundRect([float]$x, [float]$y, [float]$w, [float]$h, [float]$r) {
    $path = New-Object System.Drawing.Drawing2D.GraphicsPath
    $d = 2 * $r
    $path.AddArc($x, $y, $d, $d, 180, 90)
    $path.AddArc($x + $w - $d, $y, $d, $d, 270, 90)
    $path.AddArc($x + $w - $d, $y + $h - $d, $d, $d, 0, 90)
    $path.AddArc($x, $y + $h - $d, $d, $d, 90, 90)
    $path.CloseFigure()
    return $path
}

# 1. 外底：对角线渐变 深靛蓝 → 藏蓝
$bgPath = New-RoundRect 0 0 $size $size $radius
$grad = New-Object System.Drawing.Drawing2D.LinearGradientBrush(
    (New-Object System.Drawing.Point(0, 0)),
    (New-Object System.Drawing.Point($size, $size)),
    [System.Drawing.Color]::FromArgb(0x37, 0x30, 0xA3),
    [System.Drawing.Color]::FromArgb(0x1E, 0x1B, 0x4B))
$g.FillPath($grad, $bgPath)

# 2. 2x2 彩色方块网格
$tile  = 84.0    # 方块边长
$gap   = 12.0    # 间隙
$margin = ($size - ($tile * 2 + $gap)) / 2   # 38
$tr    = 18.0    # 方块圆角

# 右下角先画白方块，其余三块用彩色（靛蓝/青/琥珀）
$tl = [System.Drawing.Color]::FromArgb(0x81, 0x8C, 0xF8)  # 靛蓝 300
$trc = [System.Drawing.Color]::FromArgb(0x22, 0xD3, 0xEE) # 青 400
$bl = [System.Drawing.Color]::FromArgb(0xFB, 0xBF, 0x24)  # 琥珀 400

$x1 = $margin; $x2 = $margin + $tile + $gap
$y1 = $margin; $y2 = $margin + $tile + $gap

$g.FillPath((New-Object System.Drawing.SolidBrush($tl)),  (New-RoundRect $x1 $y1 $tile $tile $tr))
$g.FillPath((New-Object System.Drawing.SolidBrush($trc)), (New-RoundRect $x2 $y1 $tile $tile $tr))
$g.FillPath((New-Object System.Drawing.SolidBrush($bl)),  (New-RoundRect $x1 $y2 $tile $tile $tr))
$g.FillPath([System.Drawing.Brushes]::White,              (New-RoundRect $x2 $y2 $tile $tile $tr))

# 3. 白方块里的播放三角（靛蓝，光学上右移）
$cx = $x2 + $tile / 2 + 3; $cy = $y2 + $tile / 2
$tw = 16.0; $th = 22.0
$tri = New-Object System.Drawing.Drawing2D.GraphicsPath
$tri.AddPolygon(@(
    (New-Object System.Drawing.PointF(($cx - $tw / 2), ($cy - $th / 2))),
    (New-Object System.Drawing.PointF(($cx - $tw / 2), ($cy + $th / 2))),
    (New-Object System.Drawing.PointF(($cx + $tw / 2), $cy))
))
$g.FillPath((New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(0x4F, 0x46, 0xE5))), $tri)

$out = Join-Path $PSScriptRoot "icon.png"
$bmp.Save($out, [System.Drawing.Imaging.ImageFormat]::Png)
$g.Dispose()
$bmp.Dispose()
Write-Host "saved: $out"
