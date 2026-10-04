# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
import subprocess
from pathlib import Path
from PIL import Image

output_dir = Path(r"D:\Users\language_projects\.scripts\vscode")
output_dir.mkdir(parents=True, exist_ok=True)

edge = r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
if not Path(edge).is_file():
    edge = r"C:\Program Files\Microsoft\Edge\Application\msedge.exe"

# VS Code Path Data
PATH_BACK = "M96.46 10.796 75.857.876a6.23 6.23 0 0 0-7.098 1.21L29.355 38.04l45.66 34.661V27.3L45.11 50l-28.863-24.99 43.167-32.9L96.46 10.796Z"
PATH_MAIN = "M75.015 27.3v45.401l21.445 16.495A6.25 6.25 0 0 0 100 83.587V16.413a6.25 6.25 0 0 0-3.54-5.617L75.015 27.3Z"
PATH_FRONT = "m68.77 97.917 2.142 1.4a6.223 6.223 0 0 0 4.96-.19L96.46 89.22l-21.445-16.52L29.355 61.96 12.187 74.99a4.162 4.162 0 0 1-5.318-.236l-5.506-5.01a4.168 4.168 0 0 1-.004-6.162L16.247 50 1.36 36.417a4.168 4.168 0 0 1 .004-6.162l5.506-5.009a4.162 4.162 0 0 1 5.318-.236l56.582 72.907Z"

# 1. 款式 A: 科技蓝微光 (Dark Pro Blue)
svg_dark_blue = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#3F4552" stop-opacity="0.9"/>
    <stop offset="100%" stop-color="#1A1C22" stop-opacity="0.4"/>
  </linearGradient>
  <linearGradient id="blue_back" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#0E639C"/>
    <stop offset="100%" stop-color="#004E8C"/>
  </linearGradient>
  <linearGradient id="blue_main" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#1177BB"/>
    <stop offset="100%" stop-color="#005A9E"/>
  </linearGradient>
  <linearGradient id="blue_front" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#40B6FE"/>
    <stop offset="70%" stop-color="#007ACC"/>
    <stop offset="100%" stop-color="#005A9E"/>
  </linearGradient>
  <filter id="blue_glow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="8" stdDeviation="16" flood-color="#007ACC" flood-opacity="0.45"/>
    <feDropShadow dx="0" dy="16" stdDeviation="24" flood-color="#000000" flood-opacity="0.5"/>
  </filter>
</defs>

<!-- 底板：与 Zed 相同尺寸 rx=150 -->
<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<!-- VS Code 标志居中校准：原图 100x100，中心 (50, 50)，缩放 6.2 倍 -->
<g transform="translate(512, 512) scale(6.2) translate(-50.5, -50.1)" filter="url(#blue_glow)">
  <path d="{PATH_BACK}" fill="url(#blue_back)"/>
  <path d="{PATH_MAIN}" fill="url(#blue_main)"/>
  <path d="{PATH_FRONT}" fill="url(#blue_front)"/>
</g>
</svg>"""

# 2. 款式 B: 极客银白 (Dark Monochrome Silver)
svg_dark_silver = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#3F4552" stop-opacity="0.9"/>
    <stop offset="100%" stop-color="#1A1C22" stop-opacity="0.4"/>
  </linearGradient>
  <linearGradient id="silver_back" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#8B949E"/>
    <stop offset="100%" stop-color="#6E7681"/>
  </linearGradient>
  <linearGradient id="silver_main" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#C9D1D9"/>
    <stop offset="100%" stop-color="#8B949E"/>
  </linearGradient>
  <linearGradient id="silver_front" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="70%" stop-color="#E6EDF3"/>
    <stop offset="100%" stop-color="#C9D1D9"/>
  </linearGradient>
  <filter id="white_glow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="8" stdDeviation="14" flood-color="#FFFFFF" flood-opacity="0.25"/>
    <feDropShadow dx="0" dy="16" stdDeviation="24" flood-color="#000000" flood-opacity="0.5"/>
  </filter>
</defs>

<!-- 底板：与 Zed 相同尺寸 rx=150 -->
<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<g transform="translate(512, 512) scale(6.2) translate(-50.5, -50.1)" filter="url(#white_glow)">
  <path d="{PATH_BACK}" fill="url(#silver_back)"/>
  <path d="{PATH_MAIN}" fill="url(#silver_main)"/>
  <path d="{PATH_FRONT}" fill="url(#silver_front)"/>
</g>
</svg>"""

styles = [
    ("vscode_dark_blue", svg_dark_blue),
    ("vscode_dark_silver", svg_dark_silver),
]

SIZES = [16, 24, 32, 48, 64, 128, 256]

for name, svg_content in styles:
    svg_path = output_dir / f"{name}.svg"
    png_path = output_dir / f"{name}.png"
    ico_path = output_dir / f"{name}.ico"

    svg_path.write_text(svg_content, encoding="utf-8")

    cmd = [
        edge,
        "--headless",
        "--disable-gpu",
        "--default-background-color=00000000",
        "--window-size=1024,1024",
        f"--screenshot={png_path}",
        svg_path.as_uri(),
    ]
    subprocess.run(cmd, check=True)

    im = Image.open(png_path).convert("RGBA")
    frames = [im.resize((s, s), Image.LANCZOS) for s in SIZES]
    frames[-1].save(
        ico_path,
        format="ICO",
        append_images=frames[:-1],
        sizes=[(s, s) for s in SIZES],
    )
    print(f"[+] 生成完成: {name}.ico (尺寸: {im.size}, bbox: {im.getbbox()})")
