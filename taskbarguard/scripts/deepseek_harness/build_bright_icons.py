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
# 款式 7: 白金亮钛立体款 (Bright Titanium - 针对"下面暗暗的"全面提亮腹部)
# 渐变调整：
# 原款式 5 腹部到底掉到了 #555E6B (暗灰)
# 款式 7 腹部最暗处提亮至 #949EB0，增加下半身金属反光，整体亮度提升 35%
# ==============================================================================
svg_bright_titanium = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
{DEFS_COMMON}
  <!-- 白金亮钛渐变：头背纯白，腹部收在银亮钛色，不再黑沉沉 -->
  <linearGradient id="bright_titanium_metal" x1="15%" y1="10%" x2="85%" y2="90%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="30%" stop-color="#F2F5F8"/>
    <stop offset="60%" stop-color="#D0D7E2"/>
    <stop offset="85%" stop-color="#B0B9C6"/>
    <stop offset="100%" stop-color="#949EB0"/>
  </linearGradient>
  
  <!-- 金属边缘提亮高光微描边 -->
  <linearGradient id="bright_titanium_stroke" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF" stop-opacity="0.95"/>
    <stop offset="60%" stop-color="#CCD5E0" stop-opacity="0.8"/>
    <stop offset="100%" stop-color="#707A88" stop-opacity="0.6"/>
  </linearGradient>

  <!-- 浮雕环境光弥散微光 -->
  <filter id="bright_titanium_depth" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="6" stdDeviation="12" flood-color="#FFFFFF" flood-opacity="0.22"/>
    <feDropShadow dx="0" dy="16" stdDeviation="20" flood-color="#000000" flood-opacity="0.55"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<g transform="translate(506, 502) scale(0.94) translate(-512, -512)" filter="url(#bright_titanium_depth)">
  <g transform="translate(32 32) scale(0.9375) translate(-40 -32)">
    <path d="{whale_d}" fill="url(#bright_titanium_metal)" stroke="url(#bright_titanium_stroke)" stroke-width="8" stroke-linejoin="round"/>
  </g>
</g>
</svg>"""

# ==============================================================================
# 款式 8: 环形底光立体款 (Underlit Chrome - 底部增加反射地光)
# 模拟汽车级工业设计：背部天光照耀，腹部有地面反弹的银光，上下通透
# ==============================================================================
svg_underlit_chrome = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
{DEFS_COMMON}
  <!-- 环形双向光渐变：背顶纯白 -> 中腹收敛银灰 -> 最底腹部被底光打亮回到亮银白 -->
  <linearGradient id="underlit_metal" x1="30%" y1="0%" x2="70%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="35%" stop-color="#E1E6ED"/>
    <stop offset="65%" stop-color="#9BA5B4"/>
    <stop offset="90%" stop-color="#DCE3EC"/>
    <stop offset="100%" stop-color="#FFFFFF"/>
  </linearGradient>

  <linearGradient id="underlit_stroke" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF" stop-opacity="0.95"/>
    <stop offset="50%" stop-color="#A4AFBE" stop-opacity="0.7"/>
    <stop offset="100%" stop-color="#FFFFFF" stop-opacity="0.9"/>
  </linearGradient>

  <filter id="underlit_depth" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="6" stdDeviation="12" flood-color="#FFFFFF" flood-opacity="0.25"/>
    <feDropShadow dx="0" dy="16" stdDeviation="22" flood-color="#000000" flood-opacity="0.6"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<g transform="translate(506, 502) scale(0.94) translate(-512, -512)" filter="url(#underlit_depth)">
  <g transform="translate(32 32) scale(0.9375) translate(-40 -32)">
    <path d="{whale_d}" fill="url(#underlit_metal)" stroke="url(#underlit_stroke)" stroke-width="8" stroke-linejoin="round"/>
  </g>
</g>
</svg>"""

new_styles = [
    ("7_bright_titanium", svg_bright_titanium),
    ("8_underlit_chrome", svg_underlit_chrome),
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
    print(f"[+] 成功生成腹部提亮款: {name}.ico")
