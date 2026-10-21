# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""TaskbarGuard 专属图标渲染：盾牌 + 任务栏层叠。

产出：
    icon.png (1024x1024)
    icon.ico (256/128/64/48/32/24/16)
并同步到 internal/app/icon.ico 供 Go embed 使用。
"""

from __future__ import annotations

import shutil
import sys
from pathlib import Path

from PIL import Image, ImageDraw

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")

SIZE = 1024

BG_TOP = (30, 34, 40)
BG_BOTTOM = (14, 16, 19)
BORDER = (62, 69, 78)
STEEL = (231, 234, 237)
STEEL_DARK = (138, 147, 157)
SLOT = (26, 29, 34)
COPPER = (201, 138, 75)


def shield_points(cx: float, top: float, bottom: float, half: float, shoulder_y: float) -> list[tuple[float, float]]:
    points: list[tuple[float, float]] = [(cx - half, top), (cx + half, top), (cx + half, shoulder_y)]
    steps = 24
    for index in range(1, steps + 1):
        t = index / steps
        x = (1 - t) ** 2 * (cx + half) + 2 * (1 - t) * t * (cx + half * 0.92) + t * t * cx
        y = (1 - t) ** 2 * shoulder_y + 2 * (1 - t) * t * (bottom - 55) + t * t * bottom
        points.append((x, y))
    for index in range(1, steps + 1):
        t = index / steps
        x = (1 - t) ** 2 * cx + 2 * (1 - t) * t * (cx - half * 0.92) + t * t * (cx - half)
        y = (1 - t) ** 2 * bottom + 2 * (1 - t) * t * (bottom - 55) + t * t * shoulder_y
        points.append((x, y))
    return points


def build_icon() -> Image.Image:
    image = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))

    gradient = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    gradient_draw = ImageDraw.Draw(gradient)
    for y in range(SIZE):
        t = y / SIZE
        color = tuple(int(BG_TOP[i] + (BG_BOTTOM[i] - BG_TOP[i]) * t) for i in range(3))
        gradient_draw.line([(0, y), (SIZE, y)], fill=(*color, 255))

    mask = Image.new("L", (SIZE, SIZE), 0)
    mask_draw = ImageDraw.Draw(mask)
    padding = int(SIZE * 0.05)
    radius = int(SIZE * 0.21)
    mask_draw.rounded_rectangle([padding, padding, SIZE - padding, SIZE - padding], radius=radius, fill=255)
    image.paste(gradient, (0, 0), mask)

    draw = ImageDraw.Draw(image)
    draw.rounded_rectangle(
        [padding, padding, SIZE - padding, SIZE - padding],
        radius=radius,
        outline=(*BORDER, 225),
        width=int(SIZE * 0.011),
    )

    cx = SIZE / 2
    points = shield_points(cx=cx, top=248, bottom=800, half=236, shoulder_y=470)

    shadow = [(x, y + int(SIZE * 0.016)) for x, y in points]
    draw.polygon(shadow, fill=(8, 9, 11, 190))
    draw.polygon(points, fill=(*STEEL, 255), outline=(*STEEL_DARK, 255))

    inner = shield_points(cx=cx, top=292, bottom=752, half=188, shoulder_y=482)
    draw.polygon(inner, fill=(*SLOT, 255))

    bars = [
        (cx - 132, 396, cx + 132, 430),
        (cx - 132, 470, cx + 132, 504),
        (cx - 92, 544, cx + 92, 578),
    ]
    for box in bars:
        draw.rounded_rectangle(box, radius=17, fill=(*STEEL, 240))

    draw.ellipse(
        [cx - 26, 632 - 26, cx + 26, 632 + 26],
        fill=(*COPPER, 255),
        outline=(255, 255, 255, 230),
        width=int(SIZE * 0.005),
    )

    return image


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    icon = build_icon()

    png_path = root / "icon.png"
    icon.save(png_path, "PNG")

    ico_sizes = [256, 128, 64, 48, 32, 24, 16]
    ico_path = root / "icon.ico"
    icon.save(ico_path, format="ICO", sizes=[(size, size) for size in ico_sizes])

    app_ico_path = root / "internal" / "app" / "icon.ico"
    shutil.copyfile(ico_path, app_ico_path)

    print(f"[OK] 生成盾牌图标: {png_path}")
    print(f"[OK] 生成多尺寸 ICO: {ico_path} (尺寸: {ico_sizes})")
    print(f"[OK] 同步至 embed 资源: {app_ico_path}")


if __name__ == "__main__":
    main()
