# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
#     "numpy",
# ]
# ///
"""
TaskbarGuard Antigravity 高质感任务栏图标全套款式工业级渲染流水线。
对齐 Zed / DeepSeek Harness / Windows Terminal 的工业级黑曜石底板标准 (988x988, rx=160, 14px边框)，
生成高通透、零暗沉的 Windows 多分辨率 ICO (16, 24, 32, 48, 64, 128, 256px)。
"""

from __future__ import annotations

import base64
import io
import os
import shutil
import subprocess
from pathlib import Path
import numpy as np
from PIL import Image, ImageEnhance, ImageFilter, ImageOps

SCRIPT_DIR = Path(__file__).resolve().parent
OUTPUT_DIR = SCRIPT_DIR / "custom_icons"
OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

SRC_CANDIDATES = [
    Path(r"D:\Users\language_projects\.thirdparty\Antigravity-Hans\gui_assets\antigravity_pure_arch.png"),
    Path(r"D:\Users\language_projects\.thirdparty\Antigravity-Hans\gui_assets\antigravity_official_512.png"),
]

EDGE_PATHS = [
    r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
    r"C:\Program Files\Microsoft\Edge\Application\msedge.exe",
]
edge_exe = next((p for p in EDGE_PATHS if Path(p).is_file()), None)
if not edge_exe:
    which_edge = shutil.which("msedge") or shutil.which("chrome")
    if which_edge:
        edge_exe = which_edge
    else:
        raise FileNotFoundError("未找到 Edge/Chrome 浏览器无头引擎，无法进行矢量光追渲染。")

SIZES = [16, 24, 32, 48, 64, 128, 256]


def load_source_arch() -> Image.Image:
    src_file = next((p for p in SRC_CANDIDATES if p.is_file()), None)
    if not src_file:
        raise FileNotFoundError("未找到 Antigravity 原始引力弧图像文件")
    return Image.open(src_file).convert("RGBA")


def image_to_base64_uri(im: Image.Image) -> str:
    buf = io.BytesIO()
    im.save(buf, format="PNG")
    b64 = base64.b64encode(buf.getvalue()).decode("ascii")
    return f"data:image/png;base64,{b64}"


def create_bright_titanium_arch(arch_im: Image.Image) -> Image.Image:
    """生成对标 Windows Terminal 纯白明亮、高对比度、零发暗的白金亮钛引力弧 (Bright Titanium)。"""
    arr = np.array(arch_im, dtype=np.float32)
    orig_alpha = arr[:, :, 3] / 255.0
    h, w = arr.shape[:2]

    # 上下坐标
    y_coords = np.linspace(0.0, 1.0, h, dtype=np.float32)[:, None]
    y_grid = np.repeat(y_coords, w, axis=1)

    # 极高反差白金曲线：整体全部在 0.90~1.0 之间高亮摆动，绝无暗沉！
    rebound = 1.0 - np.sin(y_grid * np.pi * 0.75) * 0.09
    rebound = np.clip(rebound, 0.91, 1.0)

    # 提取边缘轮廓微浮雕
    alpha_im = Image.fromarray((orig_alpha * 255).astype(np.uint8), mode="L")
    dilated = alpha_im.filter(ImageFilter.MaxFilter(3))
    eroded = alpha_im.filter(ImageFilter.MinFilter(3))
    edge = np.array(dilated, dtype=np.float32) - np.array(eroded, dtype=np.float32)
    edge_mask = np.clip(edge / 255.0 * 2.0, 0.0, 1.0)

    r = rebound * 255.0
    g = (rebound * 0.98 + 0.02) * 255.0
    b = (rebound * 0.99 + 0.01) * 255.0

    # 边缘强化到 255 纯白镜面高光
    r = r * (1.0 - edge_mask) + 255.0 * edge_mask
    g = g * (1.0 - edge_mask) + 255.0 * edge_mask
    b = b * (1.0 - edge_mask) + 255.0 * edge_mask

    out = np.zeros((h, w, 4), dtype=np.uint8)
    out[:, :, 0] = np.clip(r, 0, 255).astype(np.uint8)
    out[:, :, 1] = np.clip(g, 0, 255).astype(np.uint8)
    out[:, :, 2] = np.clip(b, 0, 255).astype(np.uint8)
    out[:, :, 3] = (orig_alpha * 255.0).astype(np.uint8)

    return Image.fromarray(out, mode="RGBA")


def create_hollow_chrome_arch(arch_im: Image.Image) -> Image.Image:
    """生成对标 DeepSeek Harness 的双轨通透线雕款 (Hollow Chrome)。"""
    arr = np.array(arch_im, dtype=np.float32)
    orig_alpha = arr[:, :, 3] / 255.0
    h, w = arr.shape[:2]

    alpha_im = Image.fromarray((orig_alpha * 255).astype(np.uint8), mode="L")
    # 外轮廓膨胀 + 内轮廓大幅腐蚀，形成镂空的厚环线框
    dilated = alpha_im.filter(ImageFilter.MaxFilter(3))
    eroded = alpha_im.filter(ImageFilter.MinFilter(15))

    # 镂空环形 mask
    ring_mask = (np.array(dilated, dtype=np.float32) - np.array(eroded, dtype=np.float32)) / 255.0
    ring_mask = np.clip(ring_mask * 1.5, 0.0, 1.0) * (orig_alpha > 0.1)

    y_coords = np.linspace(0.0, 1.0, h, dtype=np.float32)[:, None]
    y_grid = np.repeat(y_coords, w, axis=1)
    rebound = 1.0 - np.sin(y_grid * np.pi * 0.8) * 0.12

    out = np.zeros((h, w, 4), dtype=np.uint8)
    out[:, :, 0] = np.clip(rebound * 255.0, 0, 255).astype(np.uint8)
    out[:, :, 1] = np.clip((rebound * 0.98 + 0.02) * 255.0, 0, 255).astype(np.uint8)
    out[:, :, 2] = np.clip((rebound * 0.99 + 0.01) * 255.0, 0, 255).astype(np.uint8)
    out[:, :, 3] = (ring_mask * 255.0).astype(np.uint8)

    return Image.fromarray(out, mode="RGBA")


def create_chrome_arch(arch_im: Image.Image) -> Image.Image:
    """优化版：大幅提亮中腹部、强化反弹光与边缘白线的 Underlit Chrome。"""
    arr = np.array(arch_im, dtype=np.float32)
    orig_alpha = arr[:, :, 3] / 255.0
    h, w = arr.shape[:2]

    y_coords = np.linspace(0.0, 1.0, h, dtype=np.float32)[:, None]
    y_grid = np.repeat(y_coords, w, axis=1)

    # 提亮中腰腹部（从原先 0.55 拔高到 0.84，顶部 1.0，底部 0.96）
    rebound = 1.0 - np.sin(y_grid * np.pi * 0.7) * 0.16
    rebound = np.clip(rebound, 0.82, 1.0)

    # 提取边缘高光
    alpha_im = Image.fromarray((orig_alpha * 255).astype(np.uint8), mode="L")
    dilated = alpha_im.filter(ImageFilter.MaxFilter(3))
    eroded = alpha_im.filter(ImageFilter.MinFilter(3))
    edge = (np.array(dilated, dtype=np.float32) - np.array(eroded, dtype=np.float32)) / 255.0

    chrome_rgb = np.zeros((h, w, 3), dtype=np.float32)
    chrome_rgb[:, :, 0] = rebound * 255.0
    chrome_rgb[:, :, 1] = (rebound * 0.97 + 0.03) * 255.0
    chrome_rgb[:, :, 2] = (rebound * 0.99 + 0.01) * 255.0

    # 叠加高光边
    for c in range(3):
        chrome_rgb[:, :, c] = np.where(edge > 0.25, 255.0, chrome_rgb[:, :, c])

    out = np.zeros((h, w, 4), dtype=np.uint8)
    out[:, :, :3] = np.clip(chrome_rgb, 0, 255).astype(np.uint8)
    out[:, :, 3] = (orig_alpha * 255.0).astype(np.uint8)

    return Image.fromarray(out, mode="RGBA")


def create_titanium_prism_arch(arch_im: Image.Image) -> Image.Image:
    """冷光钛金属与微弱边缘棱镜折射款。"""
    arr = np.array(arch_im, dtype=np.float32)
    orig_alpha = arr[:, :, 3] / 255.0
    orig_rgb = arr[:, :, :3] / 255.0
    h, w = arr.shape[:2]

    y_coords = np.linspace(0.0, 1.0, h, dtype=np.float32)[:, None]
    y_grid = np.repeat(y_coords, w, axis=1)

    base_lum = 0.92 - y_grid * 0.15
    ti_rgb = np.stack([base_lum * 0.96, base_lum * 0.98, base_lum * 1.02], axis=-1)
    blended = ti_rgb * 0.85 + orig_rgb * 0.15

    out = np.zeros((h, w, 4), dtype=np.uint8)
    out[:, :, :3] = np.clip(blended * 255.0, 0, 255).astype(np.uint8)
    out[:, :, 3] = (orig_alpha * 255.0).astype(np.uint8)
    return Image.fromarray(out, mode="RGBA")


def create_pure_white_arch(arch_im: Image.Image) -> Image.Image:
    """纯白高对比立体浮雕款。"""
    arr = np.array(arch_im, dtype=np.float32)
    orig_alpha = arr[:, :, 3] / 255.0
    h, w = arr.shape[:2]

    y_coords = np.linspace(0.0, 1.0, h, dtype=np.float32)[:, None]
    y_grid = np.repeat(y_coords, w, axis=1)
    lum = 1.0 - y_grid * 0.10

    out = np.zeros((h, w, 4), dtype=np.uint8)
    out[:, :, :3] = (lum[:, :, None] * 255.0).astype(np.uint8)
    out[:, :, 3] = (orig_alpha * 255.0).astype(np.uint8)
    return Image.fromarray(out, mode="RGBA")


def render_svg_to_png(svg_content: str, png_path: Path):
    svg_path = png_path.with_suffix(".svg")
    svg_path.write_text(svg_content, encoding="utf-8")

    cmd = [
        edge_exe,
        "--headless",
        "--disable-gpu",
        "--default-background-color=00000000",
        "--window-size=1024,1024",
        f"--screenshot={png_path}",
        svg_path.as_uri(),
    ]
    subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        svg_path.unlink()
    except Exception:
        pass


def png_to_ico(png_path: Path, ico_path: Path):
    im = Image.open(png_path).convert("RGBA")
    frames = [im.resize((s, s), Image.Resampling.LANCZOS) for s in SIZES]
    frames[-1].save(
        ico_path,
        format="ICO",
        append_images=frames[:-1],
        sizes=[(s, s) for s in SIZES],
    )


def generate_all_icons():
    raw_arch = load_source_arch()
    print("[*] Antigravity 原始素材加载完成，开始生成包含 bright_titanium 与 hollow_chrome 的全套款式...")

    bright_titanium_arch = create_bright_titanium_arch(raw_arch)
    hollow_chrome_arch = create_hollow_chrome_arch(raw_arch)
    chrome_arch = create_chrome_arch(raw_arch)
    titanium_arch = create_titanium_prism_arch(raw_arch)
    pure_white_arch = create_pure_white_arch(raw_arch)

    # 1. 款式 1: bright_titanium (极客白金亮钛款 · 对标 Windows Terminal 纯白明亮，零暗沉 ⭐)
    bright_uri = image_to_base64_uri(bright_titanium_arch)
    svg_bright_titanium = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#4E5666" stop-opacity="0.9"/>
    <stop offset="100%" stop-color="#1C1F26" stop-opacity="0.4"/>
  </linearGradient>
  <filter id="bright_glow" x="-20%" y="-20%" width="140%" height="140%">
    <feDropShadow dx="0" dy="5" stdDeviation="14" flood-color="#FFFFFF" flood-opacity="0.38"/>
    <feDropShadow dx="0" dy="16" stdDeviation="24" flood-color="#000000" flood-opacity="0.75"/>
  </filter>
</defs>
<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>
<g filter="url(#bright_glow)">
  <image href="{bright_uri}" x="212" y="202" width="600" height="600" preserveAspectRatio="xMidYMid meet"/>
</g>
</svg>"""

    # 2. 款式 2: hollow_chrome (双轨通透线雕款 · 对标 DeepSeek 环形线条 ⭐)
    hollow_uri = image_to_base64_uri(hollow_chrome_arch)
    svg_hollow_chrome = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#4E5666" stop-opacity="0.9"/>
    <stop offset="100%" stop-color="#1C1F26" stop-opacity="0.4"/>
  </linearGradient>
  <filter id="hollow_glow" x="-20%" y="-20%" width="140%" height="140%">
    <feDropShadow dx="0" dy="4" stdDeviation="12" flood-color="#FFFFFF" flood-opacity="0.32"/>
    <feDropShadow dx="0" dy="16" stdDeviation="24" flood-color="#000000" flood-opacity="0.8"/>
  </filter>
</defs>
<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>
<g filter="url(#hollow_glow)">
  <image href="{hollow_uri}" x="212" y="202" width="600" height="600" preserveAspectRatio="xMidYMid meet"/>
</g>
</svg>"""

    # 3. 款式 3: underlit_chrome (高反差通透底光铬金属款 · 提亮升级)
    chrome_uri = image_to_base64_uri(chrome_arch)
    svg_underlit_chrome = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#4E5666" stop-opacity="0.9"/>
    <stop offset="100%" stop-color="#1C1F26" stop-opacity="0.4"/>
  </linearGradient>
  <filter id="chrome_glow" x="-20%" y="-20%" width="140%" height="140%">
    <feDropShadow dx="0" dy="4" stdDeviation="12" flood-color="#FFFFFF" flood-opacity="0.32"/>
    <feDropShadow dx="0" dy="16" stdDeviation="24" flood-color="#000000" flood-opacity="0.8"/>
  </filter>
</defs>
<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>
<g filter="url(#chrome_glow)">
  <image href="{chrome_uri}" x="212" y="202" width="600" height="600" preserveAspectRatio="xMidYMid meet"/>
</g>
</svg>"""

    # 4. 款式 4: dark_rainbow (暗黑极客彩虹款 · 保留灵魂彩虹)
    rainbow_uri = image_to_base64_uri(raw_arch)
    svg_dark_rainbow = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#4E5666" stop-opacity="0.9"/>
    <stop offset="100%" stop-color="#1C1F26" stop-opacity="0.4"/>
  </linearGradient>
  <filter id="neon_glow" x="-25%" y="-25%" width="150%" height="150%">
    <feDropShadow dx="0" dy="6" stdDeviation="16" flood-color="#2563EB" flood-opacity="0.4"/>
    <feDropShadow dx="0" dy="18" stdDeviation="26" flood-color="#000000" flood-opacity="0.8"/>
  </filter>
</defs>
<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>
<g filter="url(#neon_glow)">
  <image href="{rainbow_uri}" x="212" y="202" width="600" height="600" preserveAspectRatio="xMidYMid meet"/>
</g>
</svg>"""

    # 5. 款式 5: titanium_prism (钛金冷光棱镜款)
    ti_uri = image_to_base64_uri(titanium_arch)
    svg_titanium = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#4E5666" stop-opacity="0.9"/>
    <stop offset="100%" stop-color="#1C1F26" stop-opacity="0.4"/>
  </linearGradient>
  <filter id="ti_glow" x="-20%" y="-20%" width="140%" height="140%">
    <feDropShadow dx="0" dy="4" stdDeviation="10" flood-color="#94A3B8" flood-opacity="0.3"/>
    <feDropShadow dx="0" dy="16" stdDeviation="22" flood-color="#000000" flood-opacity="0.8"/>
  </filter>
</defs>
<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>
<g filter="url(#ti_glow)">
  <image href="{ti_uri}" x="212" y="202" width="600" height="600" preserveAspectRatio="xMidYMid meet"/>
</g>
</svg>"""

    # 6. 款式 6: pure_dark (纯黑黑曜石纯白微光款)
    white_uri = image_to_base64_uri(pure_white_arch)
    svg_pure_dark = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#4E5666" stop-opacity="0.9"/>
    <stop offset="100%" stop-color="#1C1F26" stop-opacity="0.4"/>
  </linearGradient>
  <filter id="pure_glow" x="-20%" y="-20%" width="140%" height="140%">
    <feDropShadow dx="0" dy="4" stdDeviation="10" flood-color="#FFFFFF" flood-opacity="0.25"/>
    <feDropShadow dx="0" dy="16" stdDeviation="22" flood-color="#000000" flood-opacity="0.75"/>
  </filter>
</defs>
<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>
<g filter="url(#pure_glow)">
  <image href="{white_uri}" x="212" y="202" width="600" height="600" preserveAspectRatio="xMidYMid meet"/>
</g>
</svg>"""

    # 7. 款式 7: pure_floating_rainbow (纯悬浮无底板彩虹款)
    svg_pure_floating = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <filter id="float_glow" x="-20%" y="-20%" width="140%" height="140%">
    <feDropShadow dx="0" dy="12" stdDeviation="20" flood-color="#000000" flood-opacity="0.45"/>
  </filter>
</defs>
<g filter="url(#float_glow)">
  <image href="{rainbow_uri}" x="128" y="128" width="768" height="768" preserveAspectRatio="xMidYMid meet"/>
</g>
</svg>"""

    styles = {
        "bright_titanium": svg_bright_titanium,
        "hollow_chrome": svg_hollow_chrome,
        "underlit_chrome": svg_underlit_chrome,
        "dark_rainbow": svg_dark_rainbow,
        "titanium_prism": svg_titanium,
        "pure_dark": svg_pure_dark,
        "pure_floating_rainbow": svg_pure_floating,
    }

    for name, svg in styles.items():
        png_p = OUTPUT_DIR / f"{name}.png"
        ico_p = OUTPUT_DIR / f"{name}.ico"
        print(f"[*] 渲染款式: {name} ...")
        render_svg_to_png(svg, png_p)
        png_to_ico(png_p, ico_p)
        print(f"[+] 款式生成完成: {ico_p.name} ({ico_p.stat().st_size} bytes)")

    print("[SUCCESS] Antigravity 全部款式构建完毕！")


if __name__ == "__main__":
    generate_all_icons()
