# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""agyquota 专属配额仪表图标渲染脚本。

产出（确定性，同一源码重复执行字节一致）：
    icon.png (1024x1024)
    icon.ico (Windows 多尺寸: 256/128/64/48/32/24/16)
并同步至 internal/app/icon.ico 供 go:embed 使用。
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
SCALE = 2  # 先在高分辨率绘制，再降采样，保证边缘干净。

BG_TOP = (33, 35, 41)
BG_BOTTOM = (21, 22, 27)
BORDER = (74, 79, 90)
TRACK = (63, 68, 78)
TICK = (108, 113, 124)
AMBER = (240, 164, 58)
AMBER_DEEP = (196, 118, 24)
TEAL = (88, 196, 184)
PAPER = (240, 236, 226)
HUB = (24, 25, 30)


def _lerp(a: tuple[int, int, int], b: tuple[int, int, int], t: float) -> tuple[int, int, int]:
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(3))


def build_icon() -> Image.Image:
    s = SIZE * SCALE
    img = Image.new("RGBA", (s, s), (0, 0, 0, 0))

    # 1. 圆角工业面板（垂直微渐变）
    gradient = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    gd = ImageDraw.Draw(gradient)
    for y in range(s):
        color = _lerp(BG_TOP, BG_BOTTOM, y / max(1, s - 1))
        gd.line([(0, y), (s, y)], fill=(*color, 255))

    mask = Image.new("L", (s, s), 0)
    md = ImageDraw.Draw(mask)
    pad = int(s * 0.045)
    radius = int(s * 0.22)
    md.rounded_rectangle([pad, pad, s - pad, s - pad], radius=radius, fill=255)
    img.paste(gradient, (0, 0), mask)

    d = ImageDraw.Draw(img)
    d.rounded_rectangle(
        [pad, pad, s - pad, s - pad],
        radius=radius,
        outline=(*BORDER, 235),
        width=max(2, int(s * 0.009)),
    )

    cx = cy = s // 2
    dial_r = int(s * 0.315)
    box = [cx - dial_r, cy - dial_r, cx + dial_r, cy + dial_r]
    stroke = max(4, int(s * 0.038))
    start, end = 145, 395  # 250° 扫角，开口朝下
    progress = 0.68

    # 2. 刻度轨道
    d.arc(box, start=start, end=end, fill=(*TRACK, 255), width=stroke)
    # 3. 进度弧（amber）
    d.arc(box, start=start, end=start + (end - start) * progress, fill=(*AMBER, 255), width=stroke)

    # 弧线两端圆头
    cap_r = stroke // 2
    for angle, color in ((start, TRACK), (start + (end - start) * progress, AMBER)):
        rad = math.radians(angle)
        px = cx + dial_r * math.cos(rad)
        py = cy + dial_r * math.sin(rad)
        d.ellipse([px - cap_r, py - cap_r, px + cap_r, py + cap_r], fill=(*color, 255))

    # 4. 外圈刻度线
    outer = int(s * 0.405)
    inner = int(s * 0.372)
    for i in range(28):
        angle = math.radians(start + (end - start) * i / 27)
        major = i % 7 == 0
        r0 = inner - (int(s * 0.012) if major else 0)
        tick_color = PAPER if major else TICK
        tick_alpha = 210 if major else 130
        d.line(
            [
                (cx + outer * math.cos(angle), cy + outer * math.sin(angle)),
                (cx + r0 * math.cos(angle), cy + r0 * math.sin(angle)),
            ],
            fill=(*tick_color, tick_alpha),
            width=max(2, int(s * (0.008 if major else 0.004))),
        )

    # 5. 指针
    needle_angle = math.radians(start + (end - start) * progress)
    needle_len = int(s * 0.255)
    nx = cx + needle_len * math.cos(needle_angle)
    ny = cy + needle_len * math.sin(needle_angle)
    d.line([(cx, cy), (nx, ny)], fill=(*PAPER, 255), width=max(5, int(s * 0.016)))
    d.line([(cx, cy), (nx, ny)], fill=(*AMBER_DEEP, 255), width=max(2, int(s * 0.006)))

    # 6. 轴心
    hub_r = int(s * 0.052)
    d.ellipse([cx - hub_r, cy - hub_r, cx + hub_r, cy + hub_r], fill=(*HUB, 255), outline=(*PAPER, 235), width=max(3, int(s * 0.008)))
    core_r = int(s * 0.016)
    d.ellipse([cx - core_r, cy - core_r, cx + core_r, cy + core_r], fill=(*TEAL, 255))

    # 7. 底部双桶指示条（Gemini / Claude，两条不同高度）
    bar_w = int(s * 0.075)
    bar_h = int(s * 0.026)
    gap = int(s * 0.028)
    base_y = int(s * 0.775)
    left_x = cx - gap // 2 - bar_w
    right_x = cx + gap // 2
    d.rounded_rectangle(
        [left_x, base_y, left_x + bar_w, base_y + bar_h],
        radius=bar_h // 2,
        fill=(*AMBER, 255),
    )
    d.rounded_rectangle(
        [right_x, base_y, right_x + bar_w, base_y + bar_h],
        radius=bar_h // 2,
        fill=(*TEAL, 255),
    )

    return img.resize((SIZE, SIZE), Image.LANCZOS)


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    icon = build_icon()

    png_path = root / "icon.png"
    icon.save(png_path, "PNG")

    ico_sizes = [256, 128, 64, 48, 32, 24, 16]
    ico_path = root / "icon.ico"
    icon.save(ico_path, format="ICO", sizes=[(s, s) for s in ico_sizes])

    app_ico_path = root / "internal" / "app" / "icon.ico"
    app_ico_path.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(ico_path, app_ico_path)

    print(f"[OK] 生成配额仪表图标: {png_path}")
    print(f"[OK] 生成多尺寸 Windows 图标: {ico_path} (尺寸: {ico_sizes})")
    print(f"[OK] 同步至内部 embed 资源: {app_ico_path}")


if __name__ == "__main__":
    main()
