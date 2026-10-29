# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""NutVault 专属文件保险箱图标渲染脚本。

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

# 色彩定义：深空灰工业底板 + 琥珀坚果棕金 + 云朵天蓝
BG_TOP = (24, 27, 36)
BG_BOTTOM = (15, 17, 23)
BORDER_COLOR = (42, 48, 66)
GOLD_PRIMARY = (245, 158, 11)
GOLD_LIGHT = (251, 191, 36)
GOLD_DARK = (180, 83, 9)
SKY_BLUE = (59, 130, 246)
STEEL_WHITE = (241, 245, 249)


def build_icon() -> Image.Image:
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    grad = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    gd = ImageDraw.Draw(grad)

    # 1. 底板渐变 (圆角深色底座)
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

    draw = ImageDraw.Draw(img)
    # 边框线
    draw.rounded_rectangle(
        [padding, padding, SIZE - padding, SIZE - padding],
        radius=radius,
        outline=BORDER_COLOR,
        width=12,
    )

    # 2. 绘制保险箱与坚果云图腾
    center_x, center_y = SIZE // 2, SIZE // 2

    # 绘制外层坚果/盾形外轮廓
    shield_pts = [
        (center_x, center_y - 280),
        (center_x + 240, center_y - 140),
        (center_x + 180, center_y + 160),
        (center_x, center_y + 300),
        (center_x - 180, center_y + 160),
        (center_x - 240, center_y - 140),
    ]
    draw.polygon(shield_pts, fill=(28, 33, 46), outline=GOLD_PRIMARY, width=16)

    # 内部金色坚果盖弧线
    cap_box = [center_x - 160, center_y - 240, center_x + 160, center_y - 60]
    draw.chord(cap_box, start=180, end=0, fill=GOLD_PRIMARY, outline=GOLD_LIGHT, width=8)

    # 内部金库核心圆环 (密码盘)
    vault_r = 110
    draw.ellipse(
        [center_x - vault_r, center_y + 20 - vault_r, center_x + vault_r, center_y + 20 + vault_r],
        fill=(18, 21, 30),
        outline=SKY_BLUE,
        width=12,
    )

    # 密码盘刻度与同步箭头点
    for angle_deg in range(0, 360, 45):
        rad = math.radians(angle_deg)
        x1 = center_x + int((vault_r - 20) * math.cos(rad))
        y1 = center_y + 20 + int((vault_r - 20) * math.sin(rad))
        x2 = center_x + int((vault_r - 35) * math.cos(rad))
        y2 = center_y + 20 + int((vault_r - 35) * math.sin(rad))
        draw.line([(x1, y1), (x2, y2)], fill=GOLD_LIGHT, width=6)

    # 中心旋钮与坚果小苗芽
    draw.ellipse(
        [center_x - 35, center_y + 20 - 35, center_x + 35, center_y + 20 + 35],
        fill=GOLD_LIGHT,
        outline=STEEL_WHITE,
        width=4,
    )

    return img


def render(project_root: Path) -> tuple[Path, Path]:
    img = build_icon()
    png_path = project_root / "icon.png"
    ico_path = project_root / "icon.ico"
    app_ico = project_root / "internal" / "app" / "icon.ico"

    img.save(png_path, format="PNG")

    sizes = [(256, 256), (128, 128), (64, 64), (48, 48), (32, 32), (24, 24), (16, 16)]
    img.save(ico_path, format="ICO", sizes=sizes)
    shutil.copyfile(ico_path, app_ico)

    print(f"✓ 已生成: {png_path.name}")
    print(f"✓ 已生成: {ico_path.name} (含多尺寸: {', '.join(f'{w}x{h}' for w, h in sizes)})")
    print(f"✓ 已同步: internal/app/icon.ico")
    return png_path, ico_path


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    render(root)


if __name__ == "__main__":
    main()
