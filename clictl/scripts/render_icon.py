# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""clictl 专属启动台图标渲染脚本。

产出：
    icon.png (1024x1024)
    icon.ico (Windows 多尺寸: 256/128/64/48/32/24/16)
并同步到 internal/app/icon.ico 供 go:embed 使用。

设计：深灰工业面板 + 钢色刻度环 + 琥珀播放键，表达「工具注册与点火启动」。
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

BG_TOP = (30, 32, 39)
BG_BOTTOM = (15, 16, 21)
BORDER = (62, 67, 80)
STEEL_LIGHT = (214, 221, 232)
STEEL_MID = (139, 147, 163)
STEEL_DARK = (62, 69, 84)
TEAL = (34, 211, 238)
AMBER = (240, 160, 40)
AMBER_LIGHT = (255, 187, 77)


def build_icon() -> Image.Image:
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    grad = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    gd = ImageDraw.Draw(grad)
    for y in range(SIZE):
        t = y / SIZE
        color = tuple(int(BG_TOP[i] + (BG_BOTTOM[i] - BG_TOP[i]) * t) for i in range(3))
        gd.line([(0, y), (SIZE, y)], fill=(*color, 255))

    mask = Image.new("L", (SIZE, SIZE), 0)
    md = ImageDraw.Draw(mask)
    padding = int(SIZE * 0.05)
    radius = int(SIZE * 0.22)
    md.rounded_rectangle([padding, padding, SIZE - padding, SIZE - padding], radius=radius, fill=255)
    img.paste(grad, (0, 0), mask)

    d = ImageDraw.Draw(img)
    d.rounded_rectangle(
        [padding, padding, SIZE - padding, SIZE - padding],
        radius=radius,
        outline=(*BORDER, 230),
        width=int(SIZE * 0.012),
    )

    cx = cy = SIZE // 2

    # 外圈刻度：36 格，每 3 格加长，暗示"工具清单"
    dial_r = int(SIZE * 0.375)
    d.ellipse(
        [cx - dial_r, cy - dial_r, cx + dial_r, cy + dial_r],
        outline=(*TEAL, 70),
        width=int(SIZE * 0.005),
    )
    for i in range(36):
        angle = i * (2 * math.pi / 36)
        major = i % 3 == 0
        length = int(SIZE * 0.032) if major else int(SIZE * 0.015)
        r_inner = dial_r - length
        x1, y1 = cx + dial_r * math.cos(angle), cy + dial_r * math.sin(angle)
        x2, y2 = cx + r_inner * math.cos(angle), cy + r_inner * math.sin(angle)
        d.line(
            [(x1, y1), (x2, y2)],
            fill=(*TEAL, 230) if major else (*TEAL, 110),
            width=max(2, int(SIZE * 0.007) if major else int(SIZE * 0.0035)),
        )

    # 齿轮（10 齿），代表"注册 / 可执行文件"
    cx_gear = cx - int(SIZE * 0.055)
    cy_gear = cy
    teeth = 10
    r_outer = int(SIZE * 0.265)
    r_root = int(SIZE * 0.215)
    step = 2 * math.pi / teeth
    pts = []
    for i in range(teeth):
        a0 = i * step
        a1 = a0 + step * 0.18
        a2 = a0 + step * 0.36
        a3 = a0 + step * 0.50
        pts.append((cx_gear + r_root * math.cos(a0), cy_gear + r_root * math.sin(a0)))
        pts.append((cx_gear + r_outer * math.cos(a1), cy_gear + r_outer * math.sin(a1)))
        pts.append((cx_gear + r_outer * math.cos(a2), cy_gear + r_outer * math.sin(a2)))
        pts.append((cx_gear + r_root * math.cos(a3), cy_gear + r_root * math.sin(a3)))

    shadow = [(x, y + int(SIZE * 0.016)) for x, y in pts]
    d.polygon(shadow, fill=(9, 10, 14, 210))
    d.polygon(pts, fill=(*STEEL_LIGHT, 255), outline=(*STEEL_DARK, 255))

    r_hub = int(SIZE * 0.145)
    d.ellipse(
        [cx_gear - r_hub, cy_gear - r_hub, cx_gear + r_hub, cy_gear + r_hub],
        fill=(*BG_TOP, 255),
        outline=(*STEEL_MID, 255),
        width=int(SIZE * 0.008),
    )

    # 内六角螺母
    hex_r = int(SIZE * 0.075)
    hex_pts = [
        (
            cx_gear + hex_r * math.cos(math.pi / 6 + k * math.pi / 3),
            cy_gear + hex_r * math.sin(math.pi / 6 + k * math.pi / 3),
        )
        for k in range(6)
    ]
    d.polygon(hex_pts, fill=(*STEEL_MID, 255), outline=(*STEEL_LIGHT, 255))

    # 琥珀启动三角，压在齿轮右下，形成"点火"焦点
    tri_cx = cx + int(SIZE * 0.175)
    tri_cy = cy + int(SIZE * 0.155)
    tri_r = int(SIZE * 0.150)
    tri = [
        (tri_cx + tri_r * math.cos(-math.pi / 2), tri_cy + tri_r * math.sin(-math.pi / 2)),
        (tri_cx + tri_r * math.cos(math.pi / 6), tri_cy + tri_r * math.sin(math.pi / 6)),
        (tri_cx + tri_r * math.cos(5 * math.pi / 6), tri_cy + tri_r * math.sin(5 * math.pi / 6)),
    ]
    d.ellipse(
        [tri_cx - int(tri_r * 1.18), tri_cy - int(tri_r * 1.18), tri_cx + int(tri_r * 1.18), tri_cy + int(tri_r * 1.18)],
        fill=(*BG_BOTTOM, 255),
        outline=(*AMBER, 140),
        width=int(SIZE * 0.006),
    )
    d.polygon([(x, y + int(SIZE * 0.012)) for x, y in tri], fill=(8, 8, 10, 220))
    d.polygon(tri, fill=(*AMBER, 255), outline=(*AMBER_LIGHT, 255))

    return img


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    icon = build_icon()

    png_path = root / "icon.png"
    icon.save(png_path, "PNG")

    sizes = [256, 128, 64, 48, 32, 24, 16]
    ico_path = root / "icon.ico"
    icon.save(ico_path, format="ICO", sizes=[(s, s) for s in sizes])

    app_ico = root / "internal" / "app" / "icon.ico"
    shutil.copyfile(ico_path, app_ico)

    print(f"[OK] 生成 clictl 图标: {png_path}")
    print(f"[OK] 生成多尺寸 ICO: {ico_path} ({sizes})")
    print(f"[OK] 同步 embed 资源: {app_ico}")


if __name__ == "__main__":
    main()
