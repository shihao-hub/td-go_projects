# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
"""
========================================================================================
PowerShell / Windows Terminal 质感图标生成流水线 (Squircle 黑曜石与钛金属质感)
========================================================================================
款式列表：
1. underlit_chrome: 环形底光通透款 (首选 ⭐，匹配 DeepSeek/IDEA/VS Code 统一视觉)
2. dark_blue: 经典 PowerShell 科技蓝微光款 (经典微软 Azure 蓝)
3. titanium: 极客银白 / 钛金属微雕款 (纯粹金属质感)
4. cyan_matrix: 极光青蓝款 (匹配 Sublime Merge 风格)
5. pure_dark: 极简深黑哑光款 (高级黑曜石哑光)
========================================================================================
"""

import subprocess
from pathlib import Path
from PIL import Image

output_dir = Path(__file__).resolve().parent / "custom_icons"
output_dir.mkdir(parents=True, exist_ok=True)

edge = r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
if not Path(edge).is_file():
    edge = r"C:\Program Files\Microsoft\Edge\Application\msedge.exe"

# 优雅现代的 Chevron (右括号 >) 与 终端光标下划线 (_) 路径
# 中心位于 (512, 512)，布局平衡舒展
CHEVRON_PATH = (
    "M 260 280 "
    "C 260 260 282 248 300 260 "
    "L 570 492 "
    "C 584 502 584 522 570 532 "
    "L 300 764 "
    "C 282 776 260 764 260 744 "
    "L 260 655 "
    "C 260 644 266 634 275 627 "
    "L 435 512 "
    "L 275 397 "
    "C 266 390 260 380 260 369 "
    "Z"
)

# 1. 款式 1: underlit_chrome (环形底光通透款 ⭐)
svg_underlit_chrome = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#4E5666" stop-opacity="0.95"/>
    <stop offset="100%" stop-color="#1C1F26" stop-opacity="0.4"/>
  </linearGradient>
  <radialGradient id="bottom_glow" cx="50%" cy="92%" r="65%">
    <stop offset="0%" stop-color="#38BDF8" stop-opacity="0.45"/>
    <stop offset="40%" stop-color="#0284C7" stop-opacity="0.2"/>
    <stop offset="100%" stop-color="#0E0F12" stop-opacity="0"/>
  </radialGradient>
  <linearGradient id="chrome_metal" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="35%" stop-color="#E2E8F0"/>
    <stop offset="70%" stop-color="#94A3B8"/>
    <stop offset="100%" stop-color="#CBD5E1"/>
  </linearGradient>
  <linearGradient id="cursor_chrome" x1="0%" y1="0%" x2="100%" y2="0%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="100%" stop-color="#94A3B8"/>
  </linearGradient>
  <filter id="symbol_shadow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="6" stdDeviation="12" flood-color="#000000" flood-opacity="0.75"/>
    <feDropShadow dx="0" dy="16" stdDeviation="28" flood-color="#0284C7" flood-opacity="0.35"/>
  </filter>
</defs>

<!-- 底座背景与环形微光 -->
<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>
<rect x="22" y="22" width="980" height="980" rx="156" fill="url(#bottom_glow)"/>

<!-- 核心符号 (Chevron + Cursor) -->
<g filter="url(#symbol_shadow)">
  <path d="{CHEVRON_PATH}" fill="url(#chrome_metal)"/>
  <rect x="525" y="660" width="235" height="74" rx="20" fill="url(#cursor_chrome)"/>
</g>
</svg>"""

# 2. 款式 2: dark_blue (经典科技蓝微光款 ⭐)
svg_dark_blue = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#3B82F6" stop-opacity="0.8"/>
    <stop offset="100%" stop-color="#1E3A8A" stop-opacity="0.3"/>
  </linearGradient>
  <radialGradient id="center_glow" cx="45%" cy="50%" r="55%">
    <stop offset="0%" stop-color="#1D4ED8" stop-opacity="0.3"/>
    <stop offset="60%" stop-color="#0E0F12" stop-opacity="0"/>
  </radialGradient>
  <linearGradient id="blue_azure" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#60A5FA"/>
    <stop offset="50%" stop-color="#2563EB"/>
    <stop offset="100%" stop-color="#1D4ED8"/>
  </linearGradient>
  <linearGradient id="blue_cursor" x1="0%" y1="0%" x2="100%" y2="0%">
    <stop offset="0%" stop-color="#93C5FD"/>
    <stop offset="100%" stop-color="#3B82F6"/>
  </linearGradient>
  <filter id="blue_shadow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="6" stdDeviation="14" flood-color="#000000" flood-opacity="0.8"/>
    <feDropShadow dx="0" dy="12" stdDeviation="24" flood-color="#2563EB" flood-opacity="0.5"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>
<rect x="22" y="22" width="980" height="980" rx="156" fill="url(#center_glow)"/>

<g filter="url(#blue_shadow)">
  <path d="{CHEVRON_PATH}" fill="url(#blue_azure)"/>
  <rect x="525" y="660" width="235" height="74" rx="20" fill="url(#blue_cursor)"/>
</g>
</svg>"""

# 3. 款式 3: titanium (极客银白 / 钛金属微雕款)
svg_titanium = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#26282E"/>
    <stop offset="40%" stop-color="#17191D"/>
    <stop offset="100%" stop-color="#0F1013"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#64748B" stop-opacity="0.9"/>
    <stop offset="100%" stop-color="#1E293B" stop-opacity="0.4"/>
  </linearGradient>
  <linearGradient id="titanium_metal" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="40%" stop-color="#CBD5E1"/>
    <stop offset="100%" stop-color="#64748B"/>
  </linearGradient>
  <filter id="titanium_shadow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="6" stdDeviation="12" flood-color="#000000" flood-opacity="0.75"/>
    <feDropShadow dx="0" dy="16" stdDeviation="24" flood-color="#FFFFFF" flood-opacity="0.18"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<g filter="url(#titanium_shadow)">
  <path d="{CHEVRON_PATH}" fill="url(#titanium_metal)"/>
  <rect x="525" y="660" width="235" height="74" rx="20" fill="url(#titanium_metal)"/>
</g>
</svg>"""

# 4. 款式 4: cyan_matrix (极光青蓝款)
svg_cyan_matrix = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#24272D"/>
    <stop offset="40%" stop-color="#16181C"/>
    <stop offset="100%" stop-color="#0E0F12"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#06B6D4" stop-opacity="0.8"/>
    <stop offset="100%" stop-color="#083344" stop-opacity="0.3"/>
  </linearGradient>
  <radialGradient id="cyan_glow" cx="50%" cy="88%" r="60%">
    <stop offset="0%" stop-color="#06B6D4" stop-opacity="0.35"/>
    <stop offset="60%" stop-color="#0E0F12" stop-opacity="0"/>
  </radialGradient>
  <linearGradient id="cyan_metal" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#67E8F9"/>
    <stop offset="40%" stop-color="#06B6D4"/>
    <stop offset="100%" stop-color="#0891B2"/>
  </linearGradient>
  <filter id="cyan_shadow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="6" stdDeviation="12" flood-color="#000000" flood-opacity="0.8"/>
    <feDropShadow dx="0" dy="14" stdDeviation="24" flood-color="#06B6D4" flood-opacity="0.45"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>
<rect x="22" y="22" width="980" height="980" rx="156" fill="url(#cyan_glow)"/>

<g filter="url(#cyan_shadow)">
  <path d="{CHEVRON_PATH}" fill="url(#cyan_metal)"/>
  <rect x="525" y="660" width="235" height="74" rx="20" fill="url(#cyan_metal)"/>
</g>
</svg>"""

# 5. 款式 5: pure_dark (极简深黑哑光款)
svg_pure_dark = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
<defs>
  <linearGradient id="mono_bg" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#202227"/>
    <stop offset="40%" stop-color="#141518"/>
    <stop offset="100%" stop-color="#0B0C0E"/>
  </linearGradient>
  <linearGradient id="mono_border" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#334155" stop-opacity="0.75"/>
    <stop offset="100%" stop-color="#0F172A" stop-opacity="0.3"/>
  </linearGradient>
  <linearGradient id="dark_metal" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#CBD5E1"/>
    <stop offset="60%" stop-color="#94A3B8"/>
    <stop offset="100%" stop-color="#475569"/>
  </linearGradient>
  <filter id="dark_shadow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="5" stdDeviation="10" flood-color="#000000" flood-opacity="0.75"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<g filter="url(#dark_shadow)">
  <path d="{CHEVRON_PATH}" fill="url(#dark_metal)"/>
  <rect x="525" y="660" width="235" height="74" rx="20" fill="url(#dark_metal)"/>
</g>
</svg>"""

styles = [
    ("underlit_chrome", svg_underlit_chrome),
    ("dark_blue", svg_dark_blue),
    ("titanium", svg_titanium),
    ("cyan_matrix", svg_cyan_matrix),
    ("pure_dark", svg_pure_dark),
]

SIZES = [16, 24, 32, 48, 64, 128, 256]

def main():
    print(f"[*] 开始生成 PowerShell / Windows Terminal 质感图标到: {output_dir}")
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
        print(f"[+] 成功生成: {name}.ico (包含尺寸: {SIZES})")

if __name__ == "__main__":
    main()
