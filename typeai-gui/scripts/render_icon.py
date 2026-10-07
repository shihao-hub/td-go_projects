# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""typeai-gui 专属图标流水线。

用法（仓库根或本目录均可）：
    uv run scripts/render_icon.py            # 生成 build/icon.ico + build/appicon.png
    uv run scripts/render_icon.py --out DIR  # 指定输出目录（默认 build/）

产出：
    appicon.png  1024x1024 主设计稿（PNG，含透明背景）
    icon.ico     多尺寸 Windows 图标（256/128/64/48/32/24/16）

设计：深色圆角终端窗口 + 顶部三色圆点 + 发光 ">_" 提示符与青紫渐变光标块，
呼应"typeai 桌面终端壳"的产品形态；AI 光标用青→紫渐变辉光表达智能感。
每次变更本脚本时请同步更新本说明。
"""

from __future__ import annotations

import argparse
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter

SIZE = 1024

# 配色（Tokyo Night 风：深蓝黑底 + 青紫高光）
WIN_TOP = (26, 27, 38)
WIN_BOTTOM = (22, 22, 30)
BORDER = (58, 62, 88)
DOT_RED = (255, 95, 87)
DOT_YELLOW = (254, 188, 46)
DOT_GREEN = (40, 200, 64)
PROMPT = (138, 200, 255)  # >_ 提示符亮蓝
GLOW_CYAN = (122, 162, 247)
GLOW_PURPLE = (187, 154, 247)


def rounded_gradient_window(size: int) -> Image.Image:
    """深色渐变圆角窗口底板（含细描边）。"""
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    grad = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    gd = ImageDraw.Draw(grad)
    top, bottom = WIN_TOP, WIN_BOTTOM
    for y in range(size):
        t = y / size
        color = tuple(int(top[i] + (bottom[i] - top[i]) * t) for i in range(3))
        gd.line([(0, y), (size, y)], fill=(*color, 255))

    mask = Image.new("L", (size, size), 0)
    md = ImageDraw.Draw(mask)
    radius = int(size * 0.16)
    md.rounded_rectangle(
        [int(size * 0.06), int(size * 0.09), int(size * 0.94), int(size * 0.91)],
        radius=radius,
        fill=255,
    )
    img.paste(grad, (0, 0), mask)

    d = ImageDraw.Draw(img)
    d.rounded_rectangle(
        [int(size * 0.06), int(size * 0.09), int(size * 0.94), int(size * 0.91)],
        radius=radius,
        outline=(*BORDER, 255),
        width=max(4, size // 256),
    )
    return img, d


def draw_titlebar(img: Image.Image, d: ImageDraw.Draw) -> None:
    """顶部三色窗口圆点。"""
    size = img.size[0]
    y = int(size * 0.20)
    r = int(size * 0.032)
    for i, color in enumerate((DOT_RED, DOT_YELLOW, DOT_GREEN)):
        cx = int(size * 0.135) + i * int(r * 2.6)
        d.ellipse([cx - r, y - r, cx + r, y + r], fill=(*color, 255))


def draw_glow(img: Image.Image, box: tuple[int, int, int, int], color: tuple[int, int, int]) -> Image.Image:
    """绘制光标辉光层（放大模糊叠加）。"""
    size = img.size[0]
    glow = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    gd = ImageDraw.Draw(glow)
    cx, cy = (box[0] + box[2]) // 2, (box[1] + box[3]) // 2
    spread = int(size * 0.07)
    gd.ellipse(
        [cx - spread, cy - spread, cx + spread, cy + spread],
        fill=(*color, 170),
    )
    glow = glow.filter(ImageFilter.GaussianBlur(size // 10))
    img.alpha_composite(glow)
    return img


def draw_prompt(img: Image.Image, d: ImageDraw.Draw) -> None:
    """>_ 提示符 + 渐变光标块。"""
    size = img.size[0]
    lw = int(size * 0.075)  # 笔画宽度
    # ">" 两段粗线（圆头）
    cx0, cy0 = int(size * 0.335), int(size * 0.42)
    cx1, cy1 = int(size * 0.47), int(size * 0.545)
    cx2, cy2 = cx0, int(size * 0.67)
    d.line([(cx0, cy0), (cx1, cy1)], fill=(*PROMPT, 255), width=lw)
    d.line([(cx1, cy1), (cx2, cy2)], fill=(*PROMPT, 255), width=lw)
    for (px, py) in ((cx0, cy0), (cx1, cy1), (cx2, cy2)):
        d.ellipse([px - lw // 2, py - lw // 2, px + lw // 2, py + lw // 2], fill=(*PROMPT, 255))
    # "_" 粗横线
    ux0, ux1 = int(size * 0.55), int(size * 0.72)
    uy = int(size * 0.70)
    d.line([(ux0, uy), (ux1, uy)], fill=(*PROMPT, 255), width=lw)
    for (px, py) in ((ux0, uy), (ux1, uy)):
        d.ellipse([px - lw // 2, py - lw // 2, px + lw // 2, py + lw // 2], fill=(*PROMPT, 255))

    # 青紫渐变光标块（右下角）
    cb = [int(size * 0.76), int(size * 0.545), int(size * 0.845), int(size * 0.72)]
    img = draw_glow(img, cb, GLOW_CYAN)
    img = draw_glow(img, cb, GLOW_PURPLE)
    d = ImageDraw.Draw(img)
    # 垂直双色渐变块
    for y in range(cb[1], cb[3]):
        t = (y - cb[1]) / max(1, cb[3] - cb[1])
        color = tuple(int(GLOW_CYAN[i] + (GLOW_PURPLE[i] - GLOW_CYAN[i]) * t) for i in range(3))
        d.line([(cb[0], y), (cb[2], y)], fill=(*color, 255))
    d.rounded_rectangle(cb, radius=int(size * 0.03), outline=(255, 255, 255, 60), width=2)


def build_icon() -> Image.Image:
    img, d = rounded_gradient_window(SIZE)
    draw_titlebar(img, d)
    draw_prompt(img, d)
    return img


def main() -> None:
    parser = argparse.ArgumentParser(description="生成 typeai-gui 应用图标")
    parser.add_argument("--out", default="build", help="输出目录（默认 build/）")
    args = parser.parse_args()

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)

    icon = build_icon()
    png_path = out / "appicon.png"
    icon.save(png_path, "PNG")

    ico_sizes = [256, 128, 64, 48, 32, 24, 16]
    ico_path = out / "icon.ico"
    icon.save(ico_path, format="ICO", sizes=[(s, s) for s in ico_sizes])

    print(f"已生成 {png_path}")
    print(f"已生成 {ico_path}（尺寸 {ico_sizes}）")


if __name__ == "__main__":
    main()
