# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=11.0",
# ]
# ///
"""
TaskbarGuard Codex（OpenAI Codex / ChatGPT 桌面版）任务栏图标全套款式工业级渲染流水线。

对齐 Zed / DeepSeek Harness / Antigravity / Windows Terminal 的工业级黑曜石底板标准
(988x988, rx=160, 14px 边框)，把 OpenAI 绳结徽标重塑为白金亮钛 / 底光铬金属等金属浮雕
质感，输出 1024 母版 PNG 与 Windows 多分辨率 ICO (16, 24, 32, 48, 64, 128, 256)。

发光层不在 SVG 内使用 feDropShadow（Chromium 对叠加阴影存在 filter 区域栅格化残影），
改为 Edge 分层渲染 + Pillow 高斯泛光合成，确保四角绝对干净。
"""

from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path

from PIL import Image, ImageFilter

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")

SCRIPT_DIR = Path(__file__).resolve().parent
OUTPUT_DIR = SCRIPT_DIR / "custom_icons"
STAGE_DIR = SCRIPT_DIR / ".stage"
OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
STAGE_DIR.mkdir(parents=True, exist_ok=True)

EDGE_PATHS = [
    r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
    r"C:\Program Files\Microsoft\Edge\Application\msedge.exe",
]

SIZES = [16, 24, 32, 48, 64, 128, 256]

# OpenAI 官方绳结徽标矢量路径（24x24 视窗，Simple Icons CC0 版）
KNOT_PATH = (
    "M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9"
    "A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 "
    ".7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 "
    "0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 "
    "6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 "
    "4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826"
    "a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 "
    "4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 "
    "19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6"
    "a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865"
    "A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 "
    ".071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107"
    "-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 "
    "1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804"
    ".0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948"
    ".7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067"
    "-1.4997Z"
)

BASE_GRADIENTS = {
    "default": {
        "bg": [("#24272D", 0), ("#16181C", 40), ("#0E0F12", 100)],
        "border": [("#4E5666", 0, 0.9), ("#1C1F26", 100, 0.4)],
    },
    "dark": {
        "bg": [("#1B1E23", 0), ("#101216", 45), ("#08090B", 100)],
        "border": [("#3C434F", 0, 0.85), ("#12151A", 100, 0.35)],
    },
}

STYLES = {
    "bright_titanium": {
        "base": "default",
        "glyph": [("#FFFFFF", 0), ("#DFE5EE", 52), ("#FFFFFF", 100)],
        "edge": 0.55,
        "glow": (0.18, 4, 9),  # 不透明度、y 偏移、高斯 sigma（细线徽标收紧泛光避免糊成一团）
        "shadow": (0.72, 14, 22),
    },
    "underlit_chrome": {
        "base": "default",
        "glyph": [("#FFFFFF", 0), ("#9BA5B4", 55), ("#F6F9FF", 100)],
        "edge": 0.38,
        "glow": (0.16, 4, 9),
        "shadow": (0.76, 14, 22),
    },
    "pure_dark": {
        "base": "dark",
        "glyph": [("#FFFFFF", 0), ("#E7EBF1", 50), ("#FFFFFF", 100)],
        "edge": 0.30,
        "glow": (0.10, 3, 7),
        "shadow": (0.68, 12, 20),
    },
}

KNOT_BOX = 640
CANVAS = 1024


def find_edge() -> str:
    for path in EDGE_PATHS:
        if Path(path).is_file():
            return path
    which = shutil.which("msedge") or shutil.which("chrome")
    if which:
        return which
    raise FileNotFoundError("未找到 Edge/Chrome 浏览器无头引擎，无法进行矢量光追渲染。")


def gradient_stops(stops: list) -> str:
    parts = []
    for item in stops:
        if len(item) == 2:
            color, offset = item
            parts.append(f'<stop offset="{offset}%" stop-color="{color}"/>')
        else:
            color, offset, opacity = item
            parts.append(f'<stop offset="{offset}%" stop-color="{color}" stop-opacity="{opacity}"/>')
    return "".join(parts)


def gradient_defs(style: dict) -> str:
    base = BASE_GRADIENTS[style["base"]]
    return f"""
  <linearGradient id="bg" x1="0%" y1="0%" x2="100%" y2="100%">
    {gradient_stops(base["bg"])}
  </linearGradient>
  <linearGradient id="border" x1="0%" y1="0%" x2="100%" y2="100%">
    {gradient_stops(base["border"])}
  </linearGradient>
  <linearGradient id="glyph" x1="0%" y1="0%" x2="0%" y2="100%">
    {gradient_stops(style["glyph"])}
  </linearGradient>"""


def build_base_svg(style: dict) -> str:
    return f"""<svg width="{CANVAS}" height="{CANVAS}" viewBox="0 0 {CANVAS} {CANVAS}" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>{gradient_defs(style)}
</defs>
<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#bg)" stroke="url(#border)" stroke-width="14"/>
</svg>"""


def build_glyph_svg(style: dict) -> str:
    offset = (CANVAS - KNOT_BOX) / 2
    scale = KNOT_BOX / 24
    return f"""<svg width="{CANVAS}" height="{CANVAS}" viewBox="0 0 {CANVAS} {CANVAS}" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>{gradient_defs(style)}
</defs>
<g transform="translate({offset:g} {offset:g}) scale({scale:g})">
  <path d="{KNOT_PATH}" fill="url(#glyph)"/>
  <path d="{KNOT_PATH}" fill="none" stroke="#FFFFFF" stroke-opacity="{style["edge"]}" stroke-width="0.055"/>
</g>
</svg>"""


def render_svg_to_png(edge_exe: str, svg: str, png_path: Path) -> None:
    svg_path = png_path.with_suffix(".svg")
    svg_path.write_text(svg, encoding="utf-8")
    cmd = [
        edge_exe,
        "--headless",
        "--disable-gpu",
        "--default-background-color=00000000",
        f"--window-size={CANVAS},{CANVAS}",
        f"--screenshot={png_path}",
        svg_path.as_uri(),
    ]
    subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        svg_path.unlink()
    except OSError:
        pass


def solid_layer(size: tuple[int, int], color: tuple[int, int, int], alpha: Image.Image) -> Image.Image:
    layer = Image.new("RGBA", size, (*color, 0))
    layer.putalpha(alpha)
    return layer


def compose(base: Image.Image, glyph: Image.Image, style: dict) -> Image.Image:
    """底板 + 暗投影 + 白色泛光 + 绳结浮雕，全部在 Pillow 内完成。"""
    out = base.copy()
    alpha = glyph.getchannel("A")

    shadow_opacity, shadow_dy, shadow_sigma = style["shadow"]
    shadow_alpha = alpha.filter(ImageFilter.GaussianBlur(shadow_sigma)).point(
        lambda value: int(value * shadow_opacity)
    )
    out.alpha_composite(solid_layer(base.size, (0, 0, 0), shadow_alpha), (0, shadow_dy))

    glow_opacity, glow_dy, glow_sigma = style["glow"]
    glow_alpha = alpha.filter(ImageFilter.GaussianBlur(glow_sigma)).point(
        lambda value: int(value * glow_opacity)
    )
    out.alpha_composite(solid_layer(base.size, (255, 255, 255), glow_alpha), (0, glow_dy))

    out.alpha_composite(glyph)
    return out


def png_to_ico(png_path: Path, ico_path: Path) -> None:
    image = Image.open(png_path).convert("RGBA")
    frames = [image.resize((size, size), Image.Resampling.LANCZOS) for size in SIZES]
    frames[-1].save(
        ico_path,
        format="ICO",
        append_images=frames[:-1],
        sizes=[(size, size) for size in SIZES],
    )


def main() -> None:
    edge_exe = find_edge()
    print(f"[*] 无头渲染引擎: {edge_exe}")
    for name, style in STYLES.items():
        png_path = OUTPUT_DIR / f"{name}.png"
        ico_path = OUTPUT_DIR / f"{name}.ico"
        print(f"[*] 渲染款式: {name} ...")
        base_path = STAGE_DIR / f"{name}_base.png"
        glyph_path = STAGE_DIR / f"{name}_glyph.png"
        render_svg_to_png(edge_exe, build_base_svg(style), base_path)
        render_svg_to_png(edge_exe, build_glyph_svg(style), glyph_path)
        composed = compose(
            Image.open(base_path).convert("RGBA"),
            Image.open(glyph_path).convert("RGBA"),
            style,
        )
        composed.save(png_path, "PNG")
        png_to_ico(png_path, ico_path)
        print(f"[+] 生成完成: {png_path.name} / {ico_path.name} ({ico_path.stat().st_size} bytes)")
    for leftover in STAGE_DIR.iterdir():
        leftover.unlink()
    STAGE_DIR.rmdir()
    print("[SUCCESS] Codex 全部款式构建完毕！")


if __name__ == "__main__":
    main()
