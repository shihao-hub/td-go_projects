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

output_dir = Path(r"D:\Users\language_projects\go_projects\taskbarguard\scripts\deepseek_harness\custom_icons")
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
# 底板通用样式 (与 Zed / VS Code 100% 保持完全同尺寸)
# ==============================================================================
DEFS_COMMON = """
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#3F4552" stop-opacity="0.9"/>
    <stop offset="100%" stop-color="#1A1C22" stop-opacity="0.4"/>
  </linearGradient>
"""

# ==============================================================================
# 款式 5: 钛金属立体雕刻 (Titanium Sculpted)
# 特点：多阶金属钛银高光，背部与鱼头亮、腹部暗，带微倒角轮廓与立体阴影，比例内收至 0.94
# 完美呼应 Zed 的金属倒角与 VS Code 的分面缎带
# ==============================================================================
svg_titanium = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
{DEFS_COMMON}
  <!-- 钛银多阶金属流光渐变 -->
  <linearGradient id="titanium_metal" x1="15%" y1="10%" x2="85%" y2="90%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="25%" stop-color="#E2E7ED"/>
    <stop offset="55%" stop-color="#9BA5B4"/>
    <stop offset="85%" stop-color="#6E7887"/>
    <stop offset="100%" stop-color="#555E6B"/>
  </linearGradient>
  
  <!-- 金属外轮廓微切角微光 -->
  <linearGradient id="titanium_stroke" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF" stop-opacity="0.85"/>
    <stop offset="50%" stop-color="#8C97A5" stop-opacity="0.5"/>
    <stop offset="100%" stop-color="#2D333B" stop-opacity="0.8"/>
  </linearGradient>

  <!-- 浮雕立体悬浮阴影 -->
  <filter id="titanium_depth" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="6" stdDeviation="10" flood-color="#FFFFFF" flood-opacity="0.18"/>
    <feDropShadow dx="0" dy="18" stdDeviation="22" flood-color="#000000" flood-opacity="0.65"/>
  </filter>
</defs>

<!-- 底板：与 Zed 相同尺寸 rx=150 -->
<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<!-- 鲸鱼主体：比例内收至 0.94，留出呼吸感，增加金属立体高光与微描边 -->
<g transform="translate(506, 502) scale(0.94) translate(-512, -512)" filter="url(#titanium_depth)">
  <g transform="translate(32 32) scale(0.9375) translate(-40 -32)">
    <path d="{whale_d}" fill="url(#titanium_metal)" stroke="url(#titanium_stroke)" stroke-width="8" stroke-linejoin="round"/>
  </g>
</g>
</svg>"""

# ==============================================================================
# 款式 6: 灵动极简浮雕 (Luminous Relief)
# 特点：纯白丝绸微光（#FFFFFF -> #C9D1D9），四周空隙加大至 0.90，消除压迫感，轻盈悬浮
# ==============================================================================
svg_luminous = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
{DEFS_COMMON}
  <!-- 柔和丝绸白银微光渐变 -->
  <linearGradient id="luminous_silk" x1="20%" y1="0%" x2="80%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="60%" stop-color="#EBF0F5"/>
    <stop offset="100%" stop-color="#B8C2CC"/>
  </linearGradient>

  <!-- 柔润空间深度弥散投影 -->
  <filter id="silk_shadow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="4" stdDeviation="8" flood-color="#FFFFFF" flood-opacity="0.25"/>
    <feDropShadow dx="0" dy="16" stdDeviation="20" flood-color="#000000" flood-opacity="0.55"/>
  </filter>
</defs>

<!-- 底板：与 Zed 相同尺寸 rx=150 -->
<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<!-- 鲸鱼主体：比例内收至 0.90，居中透气，柔光质感 -->
<g transform="translate(506, 502) scale(0.90) translate(-512, -512)" filter="url(#silk_shadow)">
  <g transform="translate(32 32) scale(0.9375) translate(-40 -32)">
    <path d="{whale_d}" fill="url(#luminous_silk)"/>
  </g>
</g>
</svg>"""

new_styles = [
    ("5_titanium_sculpted", svg_titanium),
    ("6_luminous_relief", svg_luminous),
]

SIZES = [16, 24, 32, 48, 64, 128, 256]

for name, svg_content in new_styles:
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
    print(f"[+] 成功生成新质感图标: {name}.ico")
