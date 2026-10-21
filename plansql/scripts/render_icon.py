# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""plansql 专属图标渲染脚本（确定性输出）。

产出：
    icon.png (1024x1024)
    icon.ico (Windows 多尺寸: 256/128/64/48/32/24/16)
并自动同步至 internal/app/icon.ico 供 go:embed 使用。
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

BG_TOP = (26, 32, 44)
BG_BOTTOM = (14, 18, 26)
BORDER = (56, 68, 88)
PAPER = (236, 241, 248)
PAPER_SHADOW = (176, 189, 206)
BLUE = (56, 132, 255)
BLUE_LIGHT = (125, 178, 255)
TEAL = (32, 201, 176)
AMBER = (245, 185, 66)


def rounded_panel(img: Image.Image) -> None:
    grad = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    gd = ImageDraw.Draw(grad)
    for y in range(SIZE):
        t = y / SIZE
        color = tuple(int(BG_TOP[i] + (BG_BOTTOM[i] - BG_TOP[i]) * t) for i in range(3))
        gd.line([(0, y), (SIZE, y)], fill=(*color, 255))

    mask = Image.new("L", (SIZE, SIZE), 0)
    md = ImageDraw.Draw(mask)
    radius = int(SIZE * 0.21)
    padding = int(SIZE * 0.05)
    md.rounded_rectangle([padding, padding, SIZE - padding, SIZE - padding], radius=radius, fill=255)
    img.paste(grad, (0, 0), mask)

    d = ImageDraw.Draw(img)
    d.rounded_rectangle(
        [padding, padding, SIZE - padding, SIZE - padding],
        radius=radius,
        outline=(*BORDER, 235),
        width=int(SIZE * 0.012),
    )


def draw_document(d: ImageDraw.ImageDraw, x: int, y: int, w: int, h: int, offset: int) -> None:
    """一叠文档：后页投影 + 前页纸张 + 顶部标题线与正文线。"""
    d.rounded_rectangle(
        [x + offset, y + offset, x + offset + w, y + offset + h],
        radius=int(w * 0.06),
        fill=(*PAPER_SHADOW, 255),
    )
    d.rounded_rectangle(
        [x, y, x + w, y + h],
        radius=int(w * 0.06),
        fill=(*PAPER, 255),
    )

    line_w = int(w * 0.62)
    line_gap = int(h * 0.11)
    start_x = x + int(w * 0.12)
    start_y = y + int(h * 0.20)
    d.rounded_rectangle(
        [start_x, start_y, start_x + int(w * 0.40), start_y + int(h * 0.055)],
        radius=int(h * 0.027),
        fill=(*BLUE, 255),
    )
    for i in range(3):
        ly = start_y + int(h * 0.16) + i * line_gap
        d.rounded_rectangle(
            [start_x, ly, start_x + line_w - i * int(w * 0.08), ly + int(h * 0.028)],
            radius=int(h * 0.014),
            fill=(*PAPER_SHADOW, 255),
        )


def draw_database(d: ImageDraw.ImageDraw, cx: int, cy: int, w: int, h: int) -> None:
    """数据库柱体：三个椭圆叠层 + 顶面高光。"""
    rx = w // 2
    ry = int(h * 0.20)
    band = int(h * 0.22)

    body = [cx - rx, cy - h // 2, cx + rx, cy + h // 2]
    d.rounded_rectangle(
        [body[0], body[1] + ry, body[2], body[3] - ry],
        radius=ry,
        fill=(*BLUE, 255),
    )
    for i in range(1, 3):
        ey = body[1] + ry + i * band
        d.ellipse(
            [body[0], ey - ry, body[2], ey + ry],
            outline=(*BLUE_LIGHT, 255),
            width=int(SIZE * 0.008),
        )
    d.ellipse(
        [body[0], body[1], body[2], body[1] + 2 * ry],
        fill=(*BLUE_LIGHT, 255),
    )
    d.ellipse(
        [body[0] + int(w * 0.12), body[1] + int(ry * 0.35), body[2] - int(w * 0.12), body[1] + int(ry * 1.35)],
        fill=(*BG_TOP, 255),
    )


def build_icon() -> Image.Image:
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    rounded_panel(img)
    d = ImageDraw.Draw(img)

    # 文档叠层（偏左上）
    doc_w, doc_h = int(SIZE * 0.34), int(SIZE * 0.43)
    doc_x, doc_y = int(SIZE * 0.18), int(SIZE * 0.24)
    draw_document(d, doc_x, doc_y, doc_w, doc_h, int(SIZE * 0.035))

    # 数据库柱体（右下，压在文档上形成层次）
    db_w, db_h = int(SIZE * 0.30), int(SIZE * 0.38)
    db_cx = int(SIZE * 0.66)
    db_cy = int(SIZE * 0.63)
    draw_database(d, db_cx, db_cy, db_w, db_h)

    # 状态勾选徽标：代表 plan/spec 已完成
    badge_cx, badge_cy = int(SIZE * 0.36), int(SIZE * 0.73)
    badge_r = int(SIZE * 0.115)
    d.ellipse(
        [badge_cx - badge_r, badge_cy - badge_r, badge_cx + badge_r, badge_cy + badge_r],
        fill=(*TEAL, 255),
        outline=(*BG_TOP, 255),
        width=int(SIZE * 0.018),
    )
    d.line(
        [
            (badge_cx - int(badge_r * 0.45), badge_cy),
            (badge_cx - int(badge_r * 0.08), badge_cy + int(badge_r * 0.38)),
            (badge_cx + int(badge_r * 0.52), badge_cy - int(badge_r * 0.40)),
        ],
        fill=(255, 255, 255, 255),
        width=int(SIZE * 0.028),
        joint="curve",
    )

    # 左上角状态点
    d.ellipse(
        [int(SIZE * 0.14), int(SIZE * 0.13), int(SIZE * 0.20), int(SIZE * 0.19)],
        fill=(*AMBER, 255),
    )

    return img


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    icon = build_icon()

    png_path = root / "icon.png"
    icon.save(png_path, "PNG")

    sizes = [256, 128, 64, 48, 32, 24, 16]
    ico_path = root / "icon.ico"
    icon.save(ico_path, format="ICO", sizes=[(s, s) for s in sizes])

    app_ico_path = root / "internal" / "app" / "icon.ico"
    shutil.copyfile(ico_path, app_ico_path)

    print(f"[OK] 生成专属图标: {png_path}")
    print(f"[OK] 生成多尺寸 Windows 图标: {ico_path} (尺寸: {sizes})")
    print(f"[OK] 同步至内部 embed 资源: {app_ico_path}")


if __name__ == "__main__":
    main()
