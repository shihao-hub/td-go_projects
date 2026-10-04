# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
import re
import subprocess
from pathlib import Path
from PIL import Image

output_dir = Path(r"D:\Users\language_projects\.scripts\custom_icons")
output_dir.mkdir(parents=True, exist_ok=True)

edge = r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
if not Path(edge).is_file():
    edge = r"C:\Program Files\Microsoft\Edge\Application\msedge.exe"

svg_src = Path(r"D:\Users\language_projects\.thirdparty\deepseek-harness\apps\desktop\resources\icon-windows.svg")
raw_svg = svg_src.read_text(encoding="utf-8")

# 提取鲸鱼 path
whale_match = re.search(r'<path d="([^"]+)"', raw_svg)
if not whale_match:
    raise RuntimeError("无法从 icon-windows.svg 提取 path")
whale_d = whale_match.group(1)

# ==============================================================================
# 设计 1: IDE 暗黑质感方块 (Dark Pro - 完美匹配 Zed、Sublime 开发者风格)
# ==============================================================================
svg_dark_pro = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="dark_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#21242B"/>
    <stop offset="50%" stop-color="#16181D"/>
    <stop offset="100%" stop-color="#0E1013"/>
  </linearGradient>
  <linearGradient id="dark_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#3B414E" stop-opacity="0.9"/>
    <stop offset="100%" stop-color="#1E2128" stop-opacity="0.4"/>
  </linearGradient>
  <linearGradient id="blue_whale" x1="850" y1="40" x2="330" y2="800" gradientUnits="userSpaceOnUse">
    <stop offset="0%" stop-color="#5EA0FF"/>
    <stop offset="50%" stop-color="#2D7BFE"/>
    <stop offset="100%" stop-color="#155DFC"/>
  </linearGradient>
  <filter id="whale_glow" x="100" y="200" width="950" height="750" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="8" stdDeviation="16" flood-color="#155DFC" flood-opacity="0.45"/>
  </filter>
</defs>
<g transform="translate(32 32) scale(0.9375) translate(-40 -32)">
  <!-- 暗黑圆角方块背景与微高光边框 -->
  <rect rx="192" width="1024" height="1024" transform="translate(40 32)" fill="url(#dark_bg)" stroke="url(#dark_border)" stroke-width="12"/>
  <!-- 发光科技蓝官方鲸鱼 -->
  <g id="tray-glyph" filter="url(#whale_glow)">
    <path d="{whale_d}" fill="url(#blue_whale)"/>
  </g>
</g>
</svg>"""

# ==============================================================================
# 设计 2: 极简纯透底·科技蓝鲸鱼 (Pure Floating Whale - 完美呼应飞书、Chrome)
# ==============================================================================
svg_pure_whale = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="float_blue" x1="850" y1="40" x2="330" y2="800" gradientUnits="userSpaceOnUse">
    <stop offset="0%" stop-color="#5599FF"/>
    <stop offset="45%" stop-color="#1E6FFF"/>
    <stop offset="100%" stop-color="#0B4EC8"/>
  </linearGradient>
  <filter id="float_shadow" x="0" y="100" width="1024" height="900" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="16" stdDeviation="20" flood-color="#0B4EC8" flood-opacity="0.4"/>
  </filter>
</defs>
<!-- 去除所有底板，将官方鲸鱼放大居中 -->
<g transform="translate(-10, -50) scale(1.18)" filter="url(#float_shadow)">
  <path d="{whale_d}" fill="url(#float_blue)"/>
</g>
</svg>"""

# ==============================================================================
# 设计 3: 暗黑圆角方块 + 纯白微光鲸鱼 (Dark Monochrome - 极简极客风)
# ==============================================================================
svg_dark_mono = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#21242B"/>
    <stop offset="100%" stop-color="#0E1013"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#444A57" stop-opacity="0.8"/>
    <stop offset="100%" stop-color="#1E2128" stop-opacity="0.4"/>
  </linearGradient>
  <filter id="white_glow" x="100" y="200" width="950" height="750" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="6" stdDeviation="14" flood-color="#FFFFFF" flood-opacity="0.3"/>
  </filter>
</defs>
<g transform="translate(32 32) scale(0.9375) translate(-40 -32)">
  <rect rx="192" width="1024" height="1024" transform="translate(40 32)" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="12"/>
  <g filter="url(#white_glow)">
    <path d="{whale_d}" fill="#F0F3F8"/>
  </g>
</g>
</svg>"""

# ==============================================================================
# 设计 4: DeepSeek 官方深海蓝底 + 纯白雕刻鲸鱼 (Deep Blue Squircle)
# ==============================================================================
svg_deep_blue = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="blue_sq_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#2A73FF"/>
    <stop offset="60%" stop-color="#0F54E6"/>
    <stop offset="100%" stop-color="#073BB3"/>
  </linearGradient>
  <filter id="deep_blue_shadow" x="100" y="200" width="950" height="750" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="8" stdDeviation="12" flood-color="#04226A" flood-opacity="0.45"/>
  </filter>
</defs>
<g transform="translate(32 32) scale(0.9375) translate(-40 -32)">
  <rect rx="192" width="1024" height="1024" transform="translate(40 32)" fill="url(#blue_sq_bg)"/>
  <g filter="url(#deep_blue_shadow)">
    <path d="{whale_d}" fill="#FFFFFF"/>
  </g>
</g>
</svg>"""

variants = {
    "1_dark_pro_blue": ("【方案一·强烈推荐】暗黑圆角底板 + 发光科技蓝鲸鱼（完美融入 Zed & Sublime）", svg_dark_pro),
    "2_pure_floating_whale": ("【方案二·轻盈通透】纯透底·科技蓝灵动鲸鱼（完全去底板，呼应飞书与 Chrome）", svg_pure_whale),
    "3_dark_monochrome": ("【方案三·极简质感】暗黑圆角底板 + 银白立体鲸鱼（极简黑白金属风，呼应 Zed）", svg_dark_mono),
    "4_official_deep_blue": ("【方案四·官方品牌】深海蓝圆角底板 + 纯白立体鲸鱼（官方品牌色）", svg_deep_blue),
}

SIZES = [16, 24, 32, 48, 64, 128, 256]

for key, (label, svg_text) in variants.items():
    svg_file = output_dir / f"{key}.svg"
    png_file = output_dir / f"{key}.png"
    ico_file = output_dir / f"{key}.ico"

    svg_file.write_text(svg_text, encoding="utf-8")

    # 使用 Edge 截图导出超高清 PNG
    file_url = svg_file.as_uri()
    cmd = [
        edge,
        "--headless",
        "--disable-gpu",
        "--default-background-color=00000000",
        "--window-size=1024,1024",
        f"--screenshot={png_file}",
        file_url,
    ]
    subprocess.run(cmd, check=True)

    # 用 PIL 生成 Windows 规范多尺寸 ICO
    im = Image.open(png_file).convert("RGBA")
    frames = [im.resize((s, s), Image.LANCZOS) for s in SIZES]
    frames[-1].save(
        ico_file,
        format="ICO",
        append_images=frames[:-1],
        sizes=[(s, s) for s in SIZES],
    )
    print(f"[+] 成功构建 {label} -> {ico_file.name} (Bbox: {im.getbbox()}, 大小: {ico_file.stat().st_size} bytes)")
