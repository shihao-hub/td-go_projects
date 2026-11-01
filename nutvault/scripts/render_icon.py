# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""NutVault 专属文件保险箱图标渲染脚本。

设计核心：
  - 去除厚重沉闷的黑色方块外壳，改用现代应用通用的透明背景异形徽章。
  - 主体为饱满温暖的“坚果金库/时光盾牌”（Acorn Vault Shield）：
    * 顶部：坚果蒂与坚果帽（琥珀金/坚果暖金渐变），具象化“坚果”特征。
    * 身体：硬朗安全的金库盾牌（金色双层金属边框 + 沉稳高对比深邃曜石底）。
    * 核心：坚果云晴空蓝时间刻度转盘与高光金库把手旋钮。
  - 针对 Windows 任务栏系统托盘（16x16 / 24x24）做像素级 LOD 特化，杜绝小尺寸模糊发黑。

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

# 色彩体系
GOLD_BRIGHT = (252, 211, 77)     # 亮金高光
GOLD_PRIMARY = (245, 158, 11)    # 坚果暖金
GOLD_DEEP = (217, 119, 6)        # 坚果深金
GOLD_DARK = (180, 83, 9)         # 暗金/果蒂
SKY_BLUE_LIGHT = (125, 211, 252) # 坚果云高光天蓝
SKY_BLUE = (56, 189, 248)        # 坚果云标志蓝
SKY_BLUE_DARK = (2, 132, 199)    # 蓝环暗部
NAVY_BASE = (19, 25, 38)         # 金库盾体深蓝底
NAVY_DARK = (11, 15, 23)         # 核心底板深暗色
WHITE_GLOW = (255, 255, 255)     # 极点反光


def build_icon_hires(size: int = 1024) -> Image.Image:
    """绘制高分辨率母图 (透明背景矢量级位图)。"""
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    cx, cy = size / 2, size / 2

    # 1. 顶部坚果蒂 (Stem)
    stem_w, stem_h = size * 0.07, size * 0.08
    stem_box = [cx - stem_w / 2, size * 0.035, cx + stem_w / 2, size * 0.035 + stem_h]
    draw.rounded_rectangle(stem_box, radius=int(stem_w / 2), fill=GOLD_DARK, outline=GOLD_DEEP, width=int(size * 0.008))

    # 尺寸控制点
    cap_top = size * 0.09
    cap_h = size * 0.22
    cap_w = size * 0.82

    shield_top = cap_top + cap_h * 0.72
    shield_bottom = size * 0.94
    shield_w = size * 0.86

    # 2. 绘制盾牌身 (深邃金属底座)
    shield_pts = [
        (cx - shield_w / 2, shield_top),
        (cx + shield_w / 2, shield_top),
        (cx + shield_w * 0.44, shield_top + (shield_bottom - shield_top) * 0.50),
        (cx, shield_bottom),
        (cx - shield_w * 0.44, shield_top + (shield_bottom - shield_top) * 0.50),
    ]
    draw.polygon(shield_pts, fill=NAVY_BASE)

    # 盾牌外轮廓厚金边
    draw.polygon(shield_pts, outline=GOLD_PRIMARY, width=int(size * 0.036))

    # 盾牌内衬线 (增加立体倒角感)
    inner_pad = size * 0.024
    inner_pts = [
        (cx - shield_w / 2 + inner_pad, shield_top + inner_pad * 0.6),
        (cx + shield_w / 2 - inner_pad, shield_top + inner_pad * 0.6),
        (cx + shield_w * 0.44 - inner_pad * 0.8, shield_top + (shield_bottom - shield_top) * 0.50),
        (cx, shield_bottom - inner_pad * 1.3),
        (cx - shield_w * 0.44 + inner_pad * 0.8, shield_top + (shield_bottom - shield_top) * 0.50),
    ]
    draw.polygon(inner_pts, outline=GOLD_DEEP, width=int(size * 0.012))

    # 3. 坚果帽顶 (Acorn Cap)
    cap_box = [cx - cap_w / 2, cap_top, cx + cap_w / 2, cap_top + cap_h]
    draw.chord(cap_box, start=180, end=0, fill=GOLD_PRIMARY, outline=GOLD_BRIGHT, width=int(size * 0.018))

    # 帽顶分层条纹装饰 (突出坚果质感)
    for ratio in [0.72, 0.48]:
        w_sub = cap_w * ratio
        h_sub = cap_h * ratio
        sub_box = [cx - w_sub / 2, cap_top + (cap_h - h_sub) * 0.5, cx + w_sub / 2, cap_top + (cap_h - h_sub) * 0.5 + h_sub]
        draw.arc(sub_box, start=200, end=340, fill=GOLD_BRIGHT, width=int(size * 0.012))

    # 帽沿横梁金属封带 (Cap Band)
    band_box = [
        cx - cap_w * 0.53,
        cap_top + cap_h * 0.46,
        cx + cap_w * 0.53,
        cap_top + cap_h * 0.74,
    ]
    draw.rounded_rectangle(
        band_box,
        radius=int(size * 0.028),
        fill=GOLD_BRIGHT,
        outline=GOLD_DARK,
        width=int(size * 0.014),
    )

    # 4. 中央金库密码盘 (坚果云天蓝时光光环)
    core_cy = shield_top + (shield_bottom - shield_top) * 0.42
    core_r = size * 0.205

    # 密码盘底层暗池
    draw.ellipse(
        [cx - core_r, core_cy - core_r, cx + core_r, core_cy + core_r],
        fill=NAVY_DARK,
        outline=SKY_BLUE,
        width=int(size * 0.038),
    )

    # 密码盘内侧天蓝发光纤细副线
    inner_r = core_r - size * 0.026
    draw.ellipse(
        [cx - inner_r, core_cy - inner_r, cx + inner_r, core_cy + inner_r],
        outline=SKY_BLUE_LIGHT,
        width=int(size * 0.010),
    )

    # 时光机金库刻度 (8 方位)
    for angle in range(0, 360, 45):
        rad = math.radians(angle)
        r1 = core_r - size * 0.038
        r2 = core_r - size * 0.082
        x1 = cx + r1 * math.cos(rad)
        y1 = core_cy + r1 * math.sin(rad)
        x2 = cx + r2 * math.cos(rad)
        y2 = core_cy + r2 * math.sin(rad)
        draw.line([(x1, y1), (x2, y2)], fill=GOLD_BRIGHT, width=int(size * 0.020))

    # 5. 中心金库旋钮手柄 (Acorn Seed Core)
    knob_r = size * 0.080
    draw.ellipse(
        [cx - knob_r, core_cy - knob_r, cx + knob_r, core_cy + knob_r],
        fill=GOLD_PRIMARY,
        outline=GOLD_BRIGHT,
        width=int(size * 0.018),
    )

    # 中心星辉亮点
    dot_r = size * 0.026
    draw.ellipse(
        [cx - dot_r - size * 0.018, core_cy - dot_r - size * 0.018, cx + dot_r - size * 0.018, core_cy + dot_r - size * 0.018],
        fill=WHITE_GLOW,
    )

    return img


def build_icon_16() -> Image.Image:
    """专为 Windows 任务栏通知区域 (16x16) 精准定制的清晰像素图。"""
    img = Image.new("RGBA", (16, 16), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    gold = (*GOLD_PRIMARY, 255)
    gold_l = (*GOLD_BRIGHT, 255)
    gold_d = (*GOLD_DARK, 255)
    blue = (*SKY_BLUE, 255)
    navy = (*NAVY_BASE, 255)
    white = (255, 255, 255, 255)

    # 果蒂
    draw.point([(7, 0), (8, 0)], fill=gold_d)

    # 果帽
    for x in range(5, 11):
        draw.point((x, 1), fill=gold_l)
    for x in range(3, 13):
        draw.point((x, 2), fill=gold)
    for x in range(2, 14):
        draw.point((x, 3), fill=gold_l)

    # 盾体
    rows = [
        (4, 2, 13),
        (5, 2, 13),
        (6, 2, 13),
        (7, 3, 12),
        (8, 3, 12),
        (9, 4, 11),
        (10, 4, 11),
        (11, 5, 10),
        (12, 6, 9),
        (13, 7, 8),
    ]
    for y, x1, x2 in rows:
        for x in range(x1, x2 + 1):
            if x == x1 or x == x2:
                draw.point((x, y), fill=gold)
            else:
                draw.point((x, y), fill=navy)
    draw.point([(7, 14), (8, 14)], fill=gold)

    # 核心天蓝密码环
    blue_ring = [(6, 7), (9, 7), (6, 8), (9, 8), (7, 6), (8, 6), (7, 9), (8, 9)]
    draw.point(blue_ring, fill=blue)

    # 核心金钮与高光
    draw.point([(7, 7), (8, 7), (7, 8), (8, 8)], fill=gold_l)
    draw.point((7, 7), fill=white)

    return img


def build_icon_24() -> Image.Image:
    """专为 Windows 任务栏高 DPI 托盘 (24x24) 定制的清晰像素图。"""
    img = Image.new("RGBA", (24, 24), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    gold = (*GOLD_PRIMARY, 255)
    gold_l = (*GOLD_BRIGHT, 255)
    gold_d = (*GOLD_DARK, 255)
    blue = (*SKY_BLUE, 255)
    navy = (*NAVY_BASE, 255)
    white = (255, 255, 255, 255)

    # 顶部果蒂
    draw.rectangle([11, 0, 12, 1], fill=gold_d)

    # 果帽
    for x in range(8, 16):
        draw.point((x, 2), fill=gold_l)
    for x in range(5, 19):
        draw.point((x, 3), fill=gold)
    for x in range(4, 20):
        draw.point((x, 4), fill=gold)
    for x in range(3, 21):
        draw.point((x, 5), fill=gold_l)

    # 盾身
    rows = [
        (6, 3, 20),
        (7, 3, 20),
        (8, 3, 20),
        (9, 3, 20),
        (10, 4, 19),
        (11, 4, 19),
        (12, 5, 18),
        (13, 5, 18),
        (14, 6, 17),
        (15, 6, 17),
        (16, 7, 16),
        (17, 8, 15),
        (18, 9, 14),
        (19, 10, 13),
        (20, 11, 12),
        (21, 11, 12),
    ]
    for y, x1, x2 in rows:
        for x in range(x1, x2 + 1):
            if x == x1 or x == x2:
                draw.point((x, y), fill=gold)
            else:
                draw.point((x, y), fill=navy)

    # 天蓝时光圆盘 (中心 (11.5, 11.5))
    for angle in range(0, 360, 30):
        rad = math.radians(angle)
        px = int(round(11.5 + 4.2 * math.cos(rad)))
        py = int(round(11.5 + 4.2 * math.sin(rad)))
        draw.point((px, py), fill=blue)

    # 核心金钮与高光
    draw.rectangle([10, 10, 13, 13], fill=gold_l)
    draw.point([(10, 10), (11, 10)], fill=white)

    return img


def render(project_root: Path) -> tuple[Path, Path]:
    png_path = project_root / "icon.png"
    ico_path = project_root / "icon.ico"
    app_ico = project_root / "internal" / "app" / "icon.ico"

    hires = build_icon_hires(1024)
    hires.save(png_path, format="PNG")

    # 构建针对 Windows ICO 的多级分辨率图层
    layers: list[Image.Image] = []
    # 256, 128, 64, 48, 32 从高精度母图高质量采样
    for s in (256, 128, 64, 48, 32):
        layers.append(hires.resize((s, s), Image.Resampling.LANCZOS))
    # 24 和 16 采用像素级手工优化图层
    layers.append(build_icon_24())
    layers.append(build_icon_16())

    first_image = layers[0]
    extra_images = layers[1:]
    first_image.save(
        ico_path,
        format="ICO",
        append_images=extra_images,
    )
    shutil.copyfile(ico_path, app_ico)

    print(f"✓ 已生成透明背景高清应用图: {png_path.name} (1024x1024)")
    print(f"✓ 已生成 Windows 多尺寸 ICO: {ico_path.name} (包含 256/128/64/48/32 及像素优化版 24/16)")
    print(f"✓ 已同步到: {app_ico.relative_to(project_root)}")
    return png_path, ico_path


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    render(root)


if __name__ == "__main__":
    main()