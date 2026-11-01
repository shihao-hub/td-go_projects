# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""deepseeksearch 专属图标渲染：深色方底 + 放大镜 + 搜索结果线。

按目标尺寸分三档**分别绘制**（不是拿大图统一缩略）：

    FULL    >= 64  完整版：渐变镜环 + 三道递减结果线 + 圆头手柄
    SIMPLE  32~48  简化版：两道结果线，镜环与手柄加粗，去掉细部
    MICRO   <= 24  极简版：只剩粗环 + 粗手柄（保证 16px 仍像放大镜）

输出：
    icon.png                1024 原图
    icon.ico                16/24/32/48/64/128/256 全尺寸链（PE 资源）
    internal/app/icon.ico   go:embed 运行时图标（与上者逐字节一致）

确定性：纯几何常量 + 固定降采样，同一源码重复执行产出相同字节，
否则每次构建都会弄脏工作区，让 release 门禁自锁。

用法：
    uv run scripts/render_icon.py
    uv run scripts/render_icon.py --preview <目录>   # 额外导出各尺寸 PNG 供目视复核
"""

from __future__ import annotations

import argparse
import io
import struct
import sys
from dataclasses import dataclass
from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parent.parent
PNG_SIZE = 1024
ICO_SIZES = (16, 24, 32, 48, 64, 128, 256)

# ---- 调色板（深色族底板 + DeepSeek 蓝，整体不超过三个色相）----
PANEL = (21, 22, 30, 255)
PANEL_EDGE = (44, 49, 63, 255)
LENS = (28, 31, 42, 255)
BAR = (230, 237, 247, 255)
BLUE_TOP = (109, 134, 255)
BLUE_BOTTOM = (58, 84, 224)


@dataclass(frozen=True)
class Profile:
    """单档几何参数，全部为相对画布边长的归一化值。"""

    panel_inset: float
    panel_radius: float
    panel_edge: float
    lens_cx: float
    lens_cy: float
    lens_outer_r: float
    ring_width: float
    handle_width: float
    handle_ext: float
    bar_heights: tuple[float, ...]


FULL = Profile(
    panel_inset=0.094,
    panel_radius=0.164,
    panel_edge=0.011,
    lens_cx=0.445,
    lens_cy=0.435,
    lens_outer_r=0.262,
    ring_width=0.060,
    handle_width=0.074,
    handle_ext=0.190,
    bar_heights=(0.62, 0.50, 0.36),
)

SIMPLE = Profile(
    panel_inset=0.094,
    panel_radius=0.164,
    panel_edge=0.014,
    lens_cx=0.445,
    lens_cy=0.435,
    lens_outer_r=0.272,
    ring_width=0.090,
    handle_width=0.108,
    handle_ext=0.178,
    bar_heights=(0.60, 0.42),
)

MICRO = Profile(
    panel_inset=0.055,
    panel_radius=0.160,
    panel_edge=0.030,
    lens_cx=0.445,
    lens_cy=0.430,
    lens_outer_r=0.300,
    ring_width=0.150,
    handle_width=0.150,
    handle_ext=0.150,
    bar_heights=(),
)


def profile_for(size: int) -> Profile:
    if size >= 64:
        return FULL
    if size >= 32:
        return SIMPLE
    return MICRO


def supersample(size: int) -> int:
    """小尺寸靠高倍超采样保住边缘，大尺寸只需轻微抗锯齿。"""
    if size <= 24:
        return 16
    if size <= 48:
        return 8
    if size <= 128:
        return 4
    return 2


def vertical_gradient(dim: int, top: tuple[int, int, int], bottom: tuple[int, int, int]) -> Image.Image:
    image = Image.new("RGBA", (dim, dim))
    draw = ImageDraw.Draw(image)
    for y in range(dim):
        t = y / max(dim - 1, 1)
        color = tuple(round(top[i] + (bottom[i] - top[i]) * t) for i in range(3))
        draw.line([(0, y), (dim, y)], fill=(*color, 255))
    return image


def draw_frame(dim: int, profile: Profile) -> Image.Image:
    image = Image.new("RGBA", (dim, dim), (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)

    # 1) 深色圆角方底
    inset = round(dim * profile.panel_inset)
    radius = round(dim * profile.panel_radius)
    panel = (inset, inset, dim - inset, dim - inset)
    draw.rounded_rectangle(panel, radius=radius, fill=PANEL)
    draw.rounded_rectangle(panel, radius=radius, outline=PANEL_EDGE, width=max(1, round(dim * profile.panel_edge)))

    # 2) 镜片内衬：比面板略亮一档，形成玻璃质感
    cx = dim * profile.lens_cx
    cy = dim * profile.lens_cy
    outer = dim * profile.lens_outer_r
    ring = dim * profile.ring_width
    inner = outer - ring
    draw.ellipse((cx - inner, cy - inner, cx + inner, cy + inner), fill=LENS)

    # 3) 镜片内的搜索结果线：左对齐、长度递减，暗示排序
    if profile.bar_heights:
        bar_h = inner * 0.15
        gap = bar_h * 2.1
        count = len(profile.bar_heights)
        start_y = cy - gap * (count - 1) / 2
        left = cx - inner * 0.60
        for index, ratio in enumerate(profile.bar_heights):
            y = start_y + gap * index
            right = left + inner * 1.20 * ratio
            draw.rounded_rectangle((left, y - bar_h / 2, right, y + bar_h / 2), radius=bar_h / 2, fill=BAR)

    # 4) 镜环 + 手柄：同一层渐变填充，保证转角处颜色连续
    mask = Image.new("L", (dim, dim), 0)
    mask_draw = ImageDraw.Draw(mask)
    mask_draw.ellipse((cx - outer, cy - outer, cx + outer, cy + outer), fill=255)
    mask_draw.ellipse((cx - inner, cy - inner, cx + inner, cy + inner), fill=0)

    handle = dim * profile.handle_width
    # 手柄起点压在环带内、圆头恰好不越过环内缘，避免探进镜片形成异色凸起
    start_offset = (inner + handle * 0.6) * 0.7071
    start = (cx + start_offset, cy + start_offset)
    end_offset = (outer + dim * profile.handle_ext) * 0.7071
    end = (cx + end_offset, cy + end_offset)
    mask_draw.line([start, end], fill=255, width=max(1, round(handle)))
    for point in (start, end):
        mask_draw.ellipse(
            (point[0] - handle / 2, point[1] - handle / 2, point[0] + handle / 2, point[1] + handle / 2),
            fill=255,
        )

    image.paste(vertical_gradient(dim, BLUE_TOP, BLUE_BOTTOM), (0, 0), mask)
    return image


def render(size: int) -> Image.Image:
    profile = profile_for(size)
    scale = supersample(size) if size < PNG_SIZE else 1
    canvas = draw_frame(size * scale, profile)
    return canvas.resize((size, size), Image.LANCZOS)


def write_ico(path: Path, frames: list[Image.Image]) -> None:
    """手写 ICO 容器：Pillow 的 sizes= 只能从单图缩略，无法逐帧定制。"""
    payload = []
    for frame in frames:
        buffer = io.BytesIO()
        frame.save(buffer, format="PNG", optimize=True)
        payload.append(buffer.getvalue())

    header = struct.pack("<HHH", 0, 1, len(frames))
    offset = len(header) + 16 * len(frames)
    entries = bytearray()
    for frame, data in zip(frames, payload):
        side = frame.width
        entries += struct.pack(
            "<BBBBHHII",
            0 if side >= 256 else side,
            0 if side >= 256 else side,
            0,
            0,
            1,
            32,
            len(data),
            offset,
        )
        offset += len(data)

    path.write_bytes(header + bytes(entries) + b"".join(payload))


def main() -> None:
    if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")

    parser = argparse.ArgumentParser(description="渲染 deepseeksearch 专属图标")
    parser.add_argument("--preview", type=Path, default=None, help="额外导出各尺寸 PNG 的目录")
    args = parser.parse_args()

    frames = {size: render(size) for size in ICO_SIZES}
    png = render(PNG_SIZE)
    png.save(ROOT / "icon.png", "PNG")

    ico_path = ROOT / "icon.ico"
    write_ico(ico_path, [frames[size] for size in ICO_SIZES])

    app_ico = ROOT / "internal" / "app" / "icon.ico"
    app_ico.parent.mkdir(parents=True, exist_ok=True)
    app_ico.write_bytes(ico_path.read_bytes())

    print(f"[OK] icon.png  {PNG_SIZE}x{PNG_SIZE} → {ROOT / 'icon.png'}")
    print(f"[OK] icon.ico  尺寸链 {ICO_SIZES} → {ico_path}")
    print(f"[OK] embed 图标已同步 → {app_ico}")

    if args.preview:
        args.preview.mkdir(parents=True, exist_ok=True)
        for size, frame in frames.items():
            frame.save(args.preview / f"icon_{size}.png", "PNG")
        png.save(args.preview / f"icon_{PNG_SIZE}.png", "PNG")
        print(f"[OK] 预览帧 → {args.preview}")


if __name__ == "__main__":
    main()
