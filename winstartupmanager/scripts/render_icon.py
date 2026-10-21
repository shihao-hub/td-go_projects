# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""winstartupmanager 专属图标渲染流水线。

用法：
    uv run scripts/render_icon.py
产出：
    build/appicon.png (1024x1024)
    build/icon.ico (Windows 多尺寸图标: 256/128/64/48/32/24/16)
"""

from __future__ import annotations

import argparse
from pathlib import Path
from PIL import Image, ImageDraw, ImageFilter, ImageFont

SIZE = 1024

BG_TOP = (14, 165, 233)     # Sky 500
BG_BOTTOM = (37, 99, 235)   # Blue 600
DEV_BG_TOP = (100, 116, 139)   # Slate 500
DEV_BG_BOTTOM = (51, 65, 85)   # Slate 700
ROCKET_BODY = (255, 255, 255)
ROCKET_ACCENT = (239, 68, 68)  # Red 500
FIRE_ORANGE = (249, 115, 22)   # Orange 500
FIRE_YELLOW = (250, 204, 21)   # Yellow 400


def build_icon(dev: bool = False) -> Image.Image:
    bg_top, bg_bottom = (DEV_BG_TOP, DEV_BG_BOTTOM) if dev else (BG_TOP, BG_BOTTOM)
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    grad = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    gd = ImageDraw.Draw(grad)

    for y in range(SIZE):
        t = y / SIZE
        color = tuple(int(bg_top[i] + (bg_bottom[i] - bg_top[i]) * t) for i in range(3))
        gd.line([(0, y), (SIZE, y)], fill=(*color, 255))

    mask = Image.new("L", (SIZE, SIZE), 0)
    md = ImageDraw.Draw(mask)
    radius = int(SIZE * 0.22)
    padding = int(SIZE * 0.08)
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
        outline=(255, 255, 255, 80),
        width=int(SIZE * 0.012),
    )

    # 绘制象征自启与极速的火箭 / 启动雷电图形
    # 火箭主体（倾斜朝右上）
    cx, cy = SIZE // 2, SIZE // 2
    # 火箭机身
    body_poly = [
        (cx + int(SIZE * 0.22), cy - int(SIZE * 0.22)), # 尖端
        (cx + int(SIZE * 0.18), cy - int(SIZE * 0.04)),
        (cx + int(SIZE * 0.04), cy + int(SIZE * 0.12)),
        (cx - int(SIZE * 0.06), cy + int(SIZE * 0.16)),
        (cx - int(SIZE * 0.14), cy + int(SIZE * 0.14)), # 尾部中心
        (cx - int(SIZE * 0.16), cy + int(SIZE * 0.06)),
        (cx - int(SIZE * 0.12), cy - int(SIZE * 0.04)),
        (cx + int(SIZE * 0.04), cy - int(SIZE * 0.18)),
    ]
    d.polygon(body_poly, fill=(255, 255, 255, 255))

    # 左翼与右翼
    left_wing = [
        (cx - int(SIZE * 0.04), cy + int(SIZE * 0.05)),
        (cx - int(SIZE * 0.24), cy + int(SIZE * 0.14)),
        (cx - int(SIZE * 0.12), cy + int(SIZE * 0.20)),
    ]
    d.polygon(left_wing, fill=(*ROCKET_ACCENT, 255))

    right_wing = [
        (cx + int(SIZE * 0.05), cy - int(SIZE * 0.04)),
        (cx + int(SIZE * 0.14), cy - int(SIZE * 0.24)),
        (cx + int(SIZE * 0.20), cy - int(SIZE * 0.12)),
    ]
    d.polygon(right_wing, fill=(*ROCKET_ACCENT, 255))

    # 舷窗（天蓝色）
    wx, wy = cx + int(SIZE * 0.07), cy - int(SIZE * 0.07)
    wr = int(SIZE * 0.06)
    d.ellipse([wx - wr, wy - wr, wx + wr, wy + wr], fill=(14, 165, 233, 255), outline=(255, 255, 255, 255), width=int(SIZE * 0.015))

    # 尾焰（双层橙黄渐变光芒）
    flame_outer = [
        (cx - int(SIZE * 0.14), cy + int(SIZE * 0.14)),
        (cx - int(SIZE * 0.32), cy + int(SIZE * 0.32)),
        (cx - int(SIZE * 0.20), cy + int(SIZE * 0.20)),
    ]
    d.polygon(flame_outer, fill=(*FIRE_ORANGE, 240))

    flame_inner = [
        (cx - int(SIZE * 0.13), cy + int(SIZE * 0.13)),
        (cx - int(SIZE * 0.24), cy + int(SIZE * 0.24)),
        (cx - int(SIZE * 0.17), cy + int(SIZE * 0.17)),
    ]
    d.polygon(flame_inner, fill=(*FIRE_YELLOW, 255))

    if dev:
        font = ImageFont.load_default(size=int(SIZE * 0.11))
        text = "DEV"
        box = d.textbbox((0, 0), text, font=font)
        tw, th = box[2] - box[0], box[3] - box[1]
        d.rounded_rectangle(
            [int(SIZE * 0.06), int(SIZE * 0.965) - th - int(SIZE * 0.03),
             int(SIZE * 0.06) + tw + int(SIZE * 0.06), int(SIZE * 0.965)],
            radius=int(SIZE * 0.02),
            fill=(15, 23, 42, 235),
        )
        d.text(
            (int(SIZE * 0.09), int(SIZE * 0.965) - th - int(SIZE * 0.035)),
            text,
            font=font,
            fill=(250, 250, 250, 255),
        )

    return img


def main() -> None:
    parser = argparse.ArgumentParser(description="生成 winstartupmanager 应用图标")
    parser.add_argument("--out", default="build", help="输出目录（默认 build/）")
    parser.add_argument("--dev", action="store_true", help="生成 dev 通道图标（灰蓝配色 + DEV 标记）")
    args = parser.parse_args()

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)

    icon = build_icon(dev=args.dev)
    suffix = "_dev" if args.dev else ""
    png_path = out / f"appicon{suffix}.png"
    icon.save(png_path, "PNG")

    ico_sizes = [256, 128, 64, 48, 32, 24, 16]
    ico_path = out / f"icon{suffix}.ico"
    icon.save(ico_path, format="ICO", sizes=[(s, s) for s in ico_sizes])

    print(f"已生成 {png_path}")
    print(f"已生成 {ico_path}（尺寸 {ico_sizes}）")


if __name__ == "__main__":
    main()