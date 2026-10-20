# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""Anatomy Manager 专属精密机械图标渲染脚本。

产出：
    icon.png (1024x1024)
    icon.ico (Windows 多尺寸图标: 256/128/64/48/32/24/16)
并自动同步至 internal/app/icon.ico。
"""

from __future__ import annotations

import math
import shutil
import sys
from pathlib import Path
from PIL import Image, ImageDraw

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")

SIZE = 1024

# 色彩定义：深空灰工业底板 + 航空钛合金银 + 工程师蓝 (Blueprint Blue) + 琥珀金 (Amber Pivot)
BG_TOP = (28, 30, 38)
BG_BOTTOM = (16, 17, 24)
BORDER_COLOR = (60, 66, 82)
CYAN_GLOW = (14, 165, 233)
CYAN_LIGHT = (56, 189, 248)
STEEL_LIGHT = (226, 232, 240)
STEEL_MID = (148, 163, 184)
STEEL_DARK = (71, 85, 105)
AMBER_PIVOT = (245, 158, 11)


def build_icon() -> Image.Image:
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    grad = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    gd = ImageDraw.Draw(grad)

    # 1. 底板渐变 (深色工业面板)
    for y in range(SIZE):
        t = y / SIZE
        color = tuple(int(BG_TOP[i] + (BG_BOTTOM[i] - BG_TOP[i]) * t) for i in range(3))
        gd.line([(0, y), (SIZE, y)], fill=(*color, 255))

    mask = Image.new("L", (SIZE, SIZE), 0)
    md = ImageDraw.Draw(mask)
    radius = int(SIZE * 0.22)
    padding = int(SIZE * 0.05)
    md.rounded_rectangle(
        [padding, padding, SIZE - padding, SIZE - padding],
        radius=radius,
        fill=255,
    )
    img.paste(grad, (0, 0), mask)

    d = ImageDraw.Draw(img)

    # 细边框高光
    d.rounded_rectangle(
        [padding, padding, SIZE - padding, SIZE - padding],
        radius=radius,
        outline=(*BORDER_COLOR, 220),
        width=int(SIZE * 0.012),
    )

    cx, cy = SIZE // 2, SIZE // 2

    # 2. 精密刻度圈 (Engineering Caliper Dial)
    dial_r = int(SIZE * 0.36)
    d.ellipse(
        [cx - dial_r, cy - dial_r, cx + dial_r, cy + dial_r],
        outline=(*CYAN_GLOW, 80),
        width=int(SIZE * 0.006),
    )
    # 刻度线 (36 格)
    for i in range(36):
        angle = i * (2 * math.pi / 36)
        is_major = (i % 3 == 0)
        len_tick = int(SIZE * 0.03) if is_major else int(SIZE * 0.016)
        r_inner = dial_r - len_tick
        x1 = cx + dial_r * math.cos(angle)
        y1 = cy + dial_r * math.sin(angle)
        x2 = cx + r_inner * math.cos(angle)
        y2 = cy + r_inner * math.sin(angle)
        tick_color = (*CYAN_LIGHT, 240) if is_major else (*CYAN_GLOW, 120)
        tick_w = max(2, int(SIZE * 0.008) if is_major else int(SIZE * 0.004))
        d.line([(x1, y1), (x2, y2)], fill=tick_color, width=tick_w)

    # 3. 精密机械齿轮 (Precision Gear - 8 齿)
    teeth_count = 8
    r_outer = int(SIZE * 0.28)
    r_inner = int(SIZE * 0.22)
    gear_pts = []
    step = 2 * math.pi / teeth_count
    for i in range(teeth_count):
        a0 = i * step
        a1 = a0 + step * 0.25
        a2 = a0 + step * 0.50
        a3 = a0 + step * 0.75
        a4 = a0 + step

        gear_pts.append((cx + r_inner * math.cos(a0), cy + r_inner * math.sin(a0)))
        gear_pts.append((cx + r_outer * math.cos(a1), cy + r_outer * math.sin(a1)))
        gear_pts.append((cx + r_outer * math.cos(a2), cy + r_outer * math.sin(a2)))
        gear_pts.append((cx + r_inner * math.cos(a3), cy + r_inner * math.sin(a3)))
        gear_pts.append((cx + r_inner * math.cos(a4), cy + r_inner * math.sin(a4)))

    # 齿轮阴影
    shadow_offset = int(SIZE * 0.018)
    shadow_pts = [(x, y + shadow_offset) for x, y in gear_pts]
    d.polygon(shadow_pts, fill=(10, 11, 16, 200))

    # 齿轮主体
    d.polygon(gear_pts, fill=(*STEEL_LIGHT, 255), outline=(*STEEL_DARK, 255))

    # 齿轮内圈槽
    r_groove = int(SIZE * 0.16)
    d.ellipse(
        [cx - r_groove, cy - r_groove, cx + r_groove, cy + r_groove],
        fill=(*BG_TOP, 255),
        outline=(*STEEL_MID, 255),
        width=int(SIZE * 0.008),
    )

    # 4. 核心轴承 (Bearing) 与 结构开孔 (Anatomy Cutout)
    cutout_count = 4
    cutout_r = int(SIZE * 0.12)
    for i in range(cutout_count):
        ca = i * (math.pi / 2) + (math.pi / 4)
        chx = cx + int(cutout_r * math.cos(ca))
        chy = cy + int(cutout_r * math.sin(ca))
        cr = int(SIZE * 0.024)
        d.ellipse([chx - cr, chy - cr, chx + cr, chy + cr], fill=(*STEEL_DARK, 255))

    # 轴心内套
    r_core = int(SIZE * 0.08)
    d.ellipse(
        [cx - r_core, cy - r_core, cx + r_core, cy + r_core],
        fill=(*CYAN_GLOW, 255),
        outline=(*CYAN_LIGHT, 255),
        width=int(SIZE * 0.006),
    )

    # 琥珀金中心精密测量基准点
    r_pivot = int(SIZE * 0.038)
    d.ellipse(
        [cx - r_pivot, cy - r_pivot, cx + r_pivot, cy + r_pivot],
        fill=(*AMBER_PIVOT, 255),
        outline=(255, 255, 255, 230),
        width=int(SIZE * 0.006),
    )

    return img


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    icon = build_icon()

    # 1. 输出 1024x1024 PNG
    png_path = root / "icon.png"
    icon.save(png_path, "PNG")

    # 2. 输出多尺寸 ICO
    ico_sizes = [256, 128, 64, 48, 32, 24, 16]
    ico_path = root / "icon.ico"
    icon.save(ico_path, format="ICO", sizes=[(s, s) for s in ico_sizes])

    # 3. 同步到 internal/app/icon.ico 供 Go embed 使用
    app_ico_path = root / "internal" / "app" / "icon.ico"
    shutil.copyfile(ico_path, app_ico_path)

    print(f"[OK] 生成专属高精图标: {png_path}")
    print(f"[OK] 生成多尺寸 Windows 图标: {ico_path} (尺寸: {ico_sizes})")
    print(f"[OK] 同步至内部 embed 资源: {app_ico_path}")


if __name__ == "__main__":
    main()
