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

whale_match = re.search(r'<path d="([^"]+)"', raw_svg)
if not whale_match:
    raise RuntimeError("无法从 icon-windows.svg 提取 path")
whale_d = whale_match.group(1)

# ==============================================================================
# 精准光学几何居中校准：
# 经检测，之前偏移量为：X偏右 +25px，Y偏下 +50.5px，四周 padding 严重不匀 (左101 右51 上241 下140)
# 修正计算：
# 1. translate 向左平移 25px，向上平移 50px
# 2. scale 保持 1.08，给四周留出等距优雅的 margin，消除贴边局促感
# 3. 产生数学与视觉双重绝对几何居中
# ==============================================================================
svg_centered_mono = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
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
  <filter id="white_glow" x="50" y="100" width="950" height="850" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="8" stdDeviation="14" flood-color="#FFFFFF" flood-opacity="0.22"/>
    <feDropShadow dx="0" dy="16" stdDeviation="24" flood-color="#000000" flood-opacity="0.45"/>
  </filter>
</defs>

<!-- 底板：与 Zed 相同尺寸 rx=150 -->
<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<!-- 鲸鱼：平移校准至画布正中央 (512, 512) -->
<g transform="translate(488, 482) scale(1.08) translate(-512, -512)" filter="url(#white_glow)">
  <g transform="translate(32 32) scale(0.9375) translate(-40 -32)">
    <path d="{whale_d}" fill="#F2F5F8"/>
  </g>
</g>
</svg>"""

out_svg = output_dir / "3_dark_monochrome_centered.svg"
out_png = output_dir / "3_dark_monochrome_centered.png"
out_ico = output_dir / "3_dark_monochrome_centered.ico"

out_svg.write_text(svg_centered_mono, encoding="utf-8")

cmd = [
    edge,
    "--headless",
    "--disable-gpu",
    "--default-background-color=00000000",
    "--window-size=1024,1024",
    f"--screenshot={out_png}",
    out_svg.as_uri(),
]
subprocess.run(cmd, check=True)

im = Image.open(out_png).convert("RGBA")
SIZES = [16, 24, 32, 48, 64, 128, 256]
frames = [im.resize((s, s), Image.LANCZOS) for s in SIZES]
frames[-1].save(
    out_ico,
    format="ICO",
    append_images=frames[:-1],
    sizes=[(s, s) for s in SIZES],
)
print(f"[+] 居中校准版图标生成成功: {out_ico} (Bbox: {im.getbbox()})")
