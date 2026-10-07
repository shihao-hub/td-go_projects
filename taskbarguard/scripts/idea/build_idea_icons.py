# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
import subprocess
from pathlib import Path
from PIL import Image

output_dir = Path(__file__).resolve().parent / "custom_icons"
output_dir.mkdir(parents=True, exist_ok=True)

edge = r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
if not Path(edge).is_file():
    edge = r"C:\Program Files\Microsoft\Edge\Application\msedge.exe"

# JetBrains IntelliJ IDEA Vector Geometry
PATH_ORANGE_STRIP = "M15.9476 5.81836L4.07215 5.8201C1.82284 5.8201 0 7.64352 0 9.89283V21.3994C0 22.5881 0.519564 23.718 1.42196 24.4918L39.5828 57.2016C40.3212 57.8341 41.2614 58.182 42.2336 58.182H54.1091C56.3584 58.182 58.1818 56.3586 58.1818 54.1093V42.6009C58.1818 41.4123 57.6623 40.2824 56.7599 39.5085L18.599 6.7993C17.8607 6.16629 16.9204 5.81894 15.9476 5.81894V5.81836Z"
PATH_CORAL_POLY = "M14.5193 5.81836H4.07273C1.82342 5.81836 0 7.64178 0 9.89109V22.9837C0 23.1763 0.0139636 23.3689 0.0407273 23.5597L5.31782 60.5035C5.60465 62.5101 7.32276 64.0002 9.34982 64.0002H25.0228C27.2727 64.0002 29.0961 62.1762 29.0956 59.9263L29.0909 41.3878C29.0909 40.9503 29.0205 40.5157 28.882 40.1008L18.3825 8.60294C17.8281 6.9401 16.2717 5.81836 14.5187 5.81836H14.5193Z"
PATH_DIAGONAL_POLY = "M59.9275 0H25.9592C24.3301 0 22.8575 0.971054 22.2157 2.46807L6.14767 39.9587C5.93065 40.4655 5.81836 41.0118 5.81836 41.5633V59.9273C5.81836 62.1766 7.64178 64 9.89109 64H27.8571C28.6617 64 29.4483 63.7615 30.118 63.3146L62.1866 41.9113C63.3189 41.1561 63.9984 39.8848 63.9984 38.5239L64.0002 4.07273C64.0002 1.82342 62.1768 0 59.9275 0Z"
PATH_LETTER_I = "M17.0001 29.3856H19.9789V19.6144H17.0001V17H25.8391V19.6144H22.8603V29.3856H25.8391V32H17.0001V29.3856Z"
PATH_LETTER_J = "M27.3391 29.3002H29.4928C29.9282 29.3002 30.3161 29.2074 30.6553 29.0218C30.9945 28.8361 31.2569 28.5737 31.4426 28.2345C31.6282 27.8953 31.721 27.508 31.721 27.072V17H34.646V27.2748C34.646 28.1749 34.4386 28.984 34.0243 29.7019C33.6101 30.4198 33.0388 30.9824 32.31 31.3892C31.5812 31.7966 30.7636 32 29.8566 32H27.3391V29.3002Z"
PATH_CURSOR_LINE = "M33 44H17V47H33V44Z"

svg_brand = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
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
  <linearGradient id="brand_poly1" x1="-0.7" y1="7.6" x2="24.1" y2="61.2" gradientUnits="userSpaceOnUse">
    <stop offset="0.1" stop-color="#FF9000"/>
    <stop offset="0.65" stop-color="#FE2857"/>
  </linearGradient>
  <linearGradient id="brand_poly2" x1="4.2" y1="60.0" x2="62.9" y2="1.3" gradientUnits="userSpaceOnUse">
    <stop offset="0.2" stop-color="#FE2857"/>
    <stop offset="0.75" stop-color="#0080FF"/>
  </linearGradient>
  <filter id="brand_glow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="6" stdDeviation="14" flood-color="#FE2857" flood-opacity="0.32"/>
    <feDropShadow dx="0" dy="14" stdDeviation="22" flood-color="#000000" flood-opacity="0.55"/>
  </filter>
  <filter id="tile_shadow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="4" stdDeviation="8" flood-color="#000000" flood-opacity="0.75"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<g transform="translate(512, 512) scale(10.6) translate(-32, -32)" filter="url(#brand_glow)">
  <path d="{PATH_ORANGE_STRIP}" fill="#FF8100"/>
  <path d="{PATH_CORAL_POLY}" fill="url(#brand_poly1)"/>
  <path d="{PATH_DIAGONAL_POLY}" fill="url(#brand_poly2)"/>

  <g filter="url(#tile_shadow)">
    <rect x="12" y="12" width="40" height="40" rx="3.5" fill="#131418" stroke="#2D3039" stroke-width="0.8"/>
    <path d="{PATH_LETTER_I}" fill="#FFFFFF"/>
    <path d="{PATH_LETTER_J}" fill="#FFFFFF"/>
    <path d="{PATH_CURSOR_LINE}" fill="#FFFFFF"/>
  </g>
</g>
</svg>"""

svg_silver = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
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
  <linearGradient id="silver_poly1" x1="-0.7" y1="7.6" x2="24.1" y2="61.2" gradientUnits="userSpaceOnUse">
    <stop offset="0.1" stop-color="#7B8492"/>
    <stop offset="0.65" stop-color="#A2ABB8"/>
  </linearGradient>
  <linearGradient id="silver_poly2" x1="4.2" y1="60.0" x2="62.9" y2="1.3" gradientUnits="userSpaceOnUse">
    <stop offset="0.2" stop-color="#A2ABB8"/>
    <stop offset="0.8" stop-color="#E2E7ED"/>
  </linearGradient>
  <filter id="silver_glow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="6" stdDeviation="14" flood-color="#FFFFFF" flood-opacity="0.22"/>
    <feDropShadow dx="0" dy="14" stdDeviation="22" flood-color="#000000" flood-opacity="0.55"/>
  </filter>
  <filter id="tile_shadow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="4" stdDeviation="8" flood-color="#000000" flood-opacity="0.75"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<g transform="translate(512, 512) scale(10.6) translate(-32, -32)" filter="url(#silver_glow)">
  <path d="{PATH_ORANGE_STRIP}" fill="#5A6270"/>
  <path d="{PATH_CORAL_POLY}" fill="url(#silver_poly1)"/>
  <path d="{PATH_DIAGONAL_POLY}" fill="url(#silver_poly2)"/>

  <g filter="url(#tile_shadow)">
    <rect x="12" y="12" width="40" height="40" rx="3.5" fill="#131418" stroke="#383D48" stroke-width="0.8"/>
    <path d="{PATH_LETTER_I}" fill="#FFFFFF"/>
    <path d="{PATH_LETTER_J}" fill="#FFFFFF"/>
    <path d="{PATH_CURSOR_LINE}" fill="#FFFFFF"/>
  </g>
</g>
</svg>"""

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
  <linearGradient id="blue_poly1" x1="-0.7" y1="7.6" x2="24.1" y2="61.2" gradientUnits="userSpaceOnUse">
    <stop offset="0.1" stop-color="#005A9E"/>
    <stop offset="0.65" stop-color="#007ACC"/>
  </linearGradient>
  <linearGradient id="blue_poly2" x1="4.2" y1="60.0" x2="62.9" y2="1.3" gradientUnits="userSpaceOnUse">
    <stop offset="0.2" stop-color="#007ACC"/>
    <stop offset="0.8" stop-color="#38B6FF"/>
  </linearGradient>
  <filter id="blue_glow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="6" stdDeviation="14" flood-color="#007ACC" flood-opacity="0.35"/>
    <feDropShadow dx="0" dy="14" stdDeviation="22" flood-color="#000000" flood-opacity="0.55"/>
  </filter>
  <filter id="tile_shadow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="4" stdDeviation="8" flood-color="#000000" flood-opacity="0.75"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<g transform="translate(512, 512) scale(10.6) translate(-32, -32)" filter="url(#blue_glow)">
  <path d="{PATH_ORANGE_STRIP}" fill="#004E8C"/>
  <path d="{PATH_CORAL_POLY}" fill="url(#blue_poly1)"/>
  <path d="{PATH_DIAGONAL_POLY}" fill="url(#blue_poly2)"/>

  <g filter="url(#tile_shadow)">
    <rect x="12" y="12" width="40" height="40" rx="3.5" fill="#0F141C" stroke="#1F364D" stroke-width="0.8"/>
    <path d="{PATH_LETTER_I}" fill="#FFFFFF"/>
    <path d="{PATH_LETTER_J}" fill="#FFFFFF"/>
    <path d="{PATH_CURSOR_LINE}" fill="#FFFFFF"/>
  </g>
</g>
</svg>"""


svg_underlit_chrome = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
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

  <!-- 8号底光环形双向渐变：顶纯白 -> 腹部银灰 -> 底部被地面底光重新照亮为纯白 -->
  <linearGradient id="underlit_metal" x1="25%" y1="0%" x2="75%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="35%" stop-color="#E5EAF0"/>
    <stop offset="65%" stop-color="#9BA5B4"/>
    <stop offset="88%" stop-color="#DCE3EC"/>
    <stop offset="100%" stop-color="#FFFFFF"/>
  </linearGradient>

  <linearGradient id="underlit_stroke" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF" stop-opacity="0.95"/>
    <stop offset="50%" stop-color="#A4AFBE" stop-opacity="0.75"/>
    <stop offset="100%" stop-color="#FFFFFF" stop-opacity="0.95"/>
  </linearGradient>

  <!-- 衬底精致斜向 JetBrains 晶体微反光倒角，营造深邃空间层次而不抢眼 -->
  <linearGradient id="crystal_facet" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#2E333D" stop-opacity="0.8"/>
    <stop offset="50%" stop-color="#1B1D22" stop-opacity="0.5"/>
    <stop offset="100%" stop-color="#121317" stop-opacity="0.9"/>
  </linearGradient>

  <filter id="underlit_glow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="8" stdDeviation="16" flood-color="#FFFFFF" flood-opacity="0.28"/>
    <feDropShadow dx="0" dy="18" stdDeviation="24" flood-color="#000000" flood-opacity="0.65"/>
  </filter>
  <filter id="crystal_shadow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="10" stdDeviation="16" flood-color="#000000" flood-opacity="0.7"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<!-- 背后 JetBrains 经典钻石折角暗晶底座（柔和低调，不形成泥泞感） -->
<g filter="url(#crystal_shadow)">
  <rect x="232" y="232" width="560" height="560" rx="90" fill="url(#crystal_facet)" stroke="#383E4B" stroke-width="6"/>
  <!-- 右上角极简光泽折角切线 -->
  <path d="M 640 234 L 790 384 L 740 434 L 590 284 Z" fill="#424A58" fill-opacity="0.25"/>
</g>

<!-- 前景主体：通透底光立雕 IJ _，体量饱满，清晰锐利 -->
<g transform="translate(512, 516) scale(18) translate(-25.8, -32)" filter="url(#underlit_glow)">
  <path d="{PATH_LETTER_I}" fill="url(#underlit_metal)" stroke="url(#underlit_stroke)" stroke-width="0.35" stroke-linejoin="round"/>
  <path d="{PATH_LETTER_J}" fill="url(#underlit_metal)" stroke="url(#underlit_stroke)" stroke-width="0.35" stroke-linejoin="round"/>
  <path d="{PATH_CURSOR_LINE}" fill="url(#underlit_metal)" stroke="url(#underlit_stroke)" stroke-width="0.35" stroke-linejoin="round"/>
</g>
</svg>"""
svg_pure_sculpted = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
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

  <linearGradient id="chrome_high" x1="20%" y1="0%" x2="80%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="30%" stop-color="#F0F4F8"/>
    <stop offset="55%" stop-color="#A2ABB8"/>
    <stop offset="85%" stop-color="#E2E7ED"/>
    <stop offset="100%" stop-color="#FFFFFF"/>
  </linearGradient>

  <linearGradient id="chrome_rim" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF" stop-opacity="1.0"/>
    <stop offset="50%" stop-color="#7A8492" stop-opacity="0.6"/>
    <stop offset="100%" stop-color="#FFFFFF" stop-opacity="0.9"/>
  </linearGradient>

  <filter id="pure_glow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="10" stdDeviation="18" flood-color="#FFFFFF" flood-opacity="0.32"/>
    <feDropShadow dx="0" dy="22" stdDeviation="28" flood-color="#000000" flood-opacity="0.75"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<!-- 纯粹徽标：放大 21 倍，占据核心黄金视觉区域，呼应 Zed 和 DBeaver 的极简单体雕塑感 -->
<g transform="translate(512, 516) scale(21.5) translate(-25.8, -32)" filter="url(#pure_glow)">
  <path d="{PATH_LETTER_I}" fill="url(#chrome_high)" stroke="url(#chrome_rim)" stroke-width="0.4" stroke-linejoin="round"/>
  <path d="{PATH_LETTER_J}" fill="url(#chrome_high)" stroke="url(#chrome_rim)" stroke-width="0.4" stroke-linejoin="round"/>
  <path d="{PATH_CURSOR_LINE}" fill="url(#chrome_high)" stroke="url(#chrome_rim)" stroke-width="0.4" stroke-linejoin="round"/>
</g>
</svg>"""
svg_titanium_prism = f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
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

  <!-- 钛金光刃渐变 -->
  <linearGradient id="blade_light" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="60%" stop-color="#CBD3DE"/>
    <stop offset="100%" stop-color="#8F99A8"/>
  </linearGradient>
  <linearGradient id="blade_mid" x1="0%" y1="100%" x2="100%" y2="0%">
    <stop offset="0%" stop-color="#6F7886"/>
    <stop offset="100%" stop-color="#A8B2C0"/>
  </linearGradient>
  <linearGradient id="core_box" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#1B1D22"/>
    <stop offset="100%" stop-color="#0B0C0E"/>
  </linearGradient>

  <filter id="prism_glow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="8" stdDeviation="16" flood-color="#FFFFFF" flood-opacity="0.25"/>
    <feDropShadow dx="0" dy="18" stdDeviation="24" flood-color="#000000" flood-opacity="0.6"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="150" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<!-- 整体放大 13.5 倍（原版仅 10.6 倍，放大 28% 消除瘦小感），让光刃与内部核心紧凑充盈 -->
<g transform="translate(512, 512) scale(13.6) translate(-32, -32)" filter="url(#prism_glow)">
  <!-- 鲜亮反光刃边 -->
  <path d="M15.9476 5.81836L4.07215 5.8201C1.82284 5.8201 0 7.64352 0 9.89283V21.3994C0 22.5881 0.519564 23.718 1.42196 24.4918L39.5828 57.2016C40.3212 57.8341 41.2614 58.182 42.2336 58.182H54.1091C56.3584 58.182 58.1818 56.3586 58.1818 54.1093V42.6009C58.1818 41.4123 57.6623 40.2824 56.7599 39.5085L18.599 6.7993C17.8607 6.16629 16.9204 5.81894 15.9476 5.81894V5.81836Z" fill="url(#blade_mid)"/>
  <path d="M59.9275 0H25.9592C24.3301 0 22.8575 0.971054 22.2157 2.46807L6.14767 39.9587C5.93065 40.4655 5.81836 41.0118 5.81836 41.5633V59.9273C5.81836 62.1766 7.64178 64 9.89109 64H27.8571C28.6617 64 29.4483 63.7615 30.118 63.3146L62.1866 41.9113C63.3189 41.1561 63.9984 39.8848 63.9984 38.5239L64.0002 4.07273C64.0002 1.82342 62.1768 0 59.9275 0Z" fill="url(#blade_light)"/>

  <!-- 居中核心卡片 -->
  <rect x="11.5" y="11.5" width="41" height="41" rx="4.5" fill="url(#core_box)" stroke="#4A5260" stroke-width="1.2"/>
  <!-- 字母与光标放大并带有微光 -->
  <g transform="translate(32, 32) scale(1.15) translate(-32, -32)">
    <path d="{PATH_LETTER_I}" fill="#FFFFFF"/>
    <path d="{PATH_LETTER_J}" fill="#FFFFFF"/>
    <path d="{PATH_CURSOR_LINE}" fill="#FFFFFF"/>
  </g>
</g>
</svg>"""

styles = [
    ("underlit_chrome", svg_underlit_chrome),
    ("pure_sculpted", svg_pure_sculpted),
    ("titanium_prism", svg_titanium_prism),
    ("brand", svg_brand),
    ("silver", svg_silver),
    ("dark_blue", svg_dark_blue),
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
