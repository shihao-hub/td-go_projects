# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""mail2feishu 专属图标渲染：绿色圆角底 + 白色信封。

产出：
    icon.png (1024x1024)
    icon.ico (256/128/64/48/32/24/16)
并同步到 internal/app/icon.ico 供 Go embed 使用。
dev 构建不调用本脚本，使用已入库的地鼠图标。
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
BG_TOP = (58, 157, 110)
BG_BOTTOM = (37, 101, 72)
WHITE = (255, 255, 255)
SHADOW = (18, 49, 35, 110)


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
    padding = int(SIZE * 0.055)
    radius = int(SIZE * 0.22)
    mask_draw.rounded_rectangle(
        [padding, padding, SIZE - padding, SIZE - padding],
        radius=radius,
        fill=255,
    )
    image.paste(gradient, (0, 0), mask)

    draw = ImageDraw.Draw(image)
    left, top, right, bottom = 250, 330, 774, 694

    draw.rounded_rectangle(
        [left + 14, top + 18, right + 14, bottom + 18],
        radius=34,
        fill=SHADOW,
    )
    draw.rounded_rectangle([left, top, right, bottom], radius=34, fill=(*WHITE, 255))
    draw.line(
        [(left + 40, top + 46), (SIZE // 2, top + 206), (right - 40, top + 46)],
        fill=(*BG_BOTTOM, 255),
        width=34,
        joint="curve",
    )
    draw.line(
        [(left + 110, bottom - 62), (right - 110, bottom - 62)],
        fill=(*BG_TOP, 120),
        width=22,
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

    print(f"[OK] 生成信封图标: {png_path}")
    print(f"[OK] 生成多尺寸 ICO: {ico_path} (尺寸: {ico_sizes})")
    print(f"[OK] 同步至 embed 资源: {app_ico_path}")


if __name__ == "__main__":
    main()
