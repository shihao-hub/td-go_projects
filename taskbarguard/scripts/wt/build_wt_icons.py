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

edge = r"C:\Program Files\Microsoft\Edge\Application\msedge.exe"
if not Path(edge).is_file():
    edge = r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"

# =========================================================================
# 优雅留白极客纯白金款 (零彩光 · 尺度恢复 1.00x 优雅留白 · 彻底根治掉色)
# =========================================================================
def get_svg_content(scale_val: float = 1.0):
    return f"""<svg width="1024" height="1024" viewBox="0 0 1024 1024" fill="none" xmlns="http://www.w3.org/2000/svg">
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

  <!-- 白金多层高对比镜面渐变 (彻底根治掉色，纯白明亮) -->
  <linearGradient id="solid_chrome" x1="20%" y1="0%" x2="80%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="30%" stop-color="#F8FAFC"/>
    <stop offset="60%" stop-color="#A0AEC0"/>
    <stop offset="90%" stop-color="#E2E8F0"/>
    <stop offset="100%" stop-color="#FFFFFF"/>
  </linearGradient>

  <!-- 光标独立纯白镜面 (绝不发暗) -->
  <linearGradient id="cursor_chrome" x1="0%" y1="0%" x2="100%" y2="0%">
    <stop offset="0%" stop-color="#FFFFFF"/>
    <stop offset="50%" stop-color="#CBD5E1"/>
    <stop offset="100%" stop-color="#FFFFFF"/>
  </linearGradient>

  <!-- 锐利微描边 -->
  <linearGradient id="solid_rim" x1="0%" y1="0%" x2="100%" y2="100%">
    <stop offset="0%" stop-color="#FFFFFF" stop-opacity="0.95"/>
    <stop offset="50%" stop-color="#94A3B8" stop-opacity="0.6"/>
    <stop offset="100%" stop-color="#FFFFFF" stop-opacity="0.85"/>
  </linearGradient>

  <!-- 纯白冷月光弥散微光 (无彩光) -->
  <filter id="solid_glow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
    <feDropShadow dx="0" dy="5" stdDeviation="12" flood-color="#FFFFFF" flood-opacity="0.25"/>
    <feDropShadow dx="0" dy="16" stdDeviation="22" flood-color="#000000" flood-opacity="0.75"/>
  </filter>
</defs>

<rect x="18" y="18" width="988" height="988" rx="160" fill="url(#mono_bg)" stroke="url(#mono_border)" stroke-width="14"/>

<!-- 优雅缩放与居中：四边保留约 24% 充裕留白，彻底告别压迫感 -->
<g transform="translate(512, 512) scale({scale_val}) translate(-512, -512)" filter="url(#solid_glow)">
  <path d="M 260 270 C 260 250 282 238 300 250 L 585 492 C 598 502 598 522 585 532 L 300 774 C 282 786 260 774 260 754 L 260 655 C 260 644 266 634 275 627 L 435 512 L 275 397 C 266 390 260 380 260 369 Z" 
        fill="url(#solid_chrome)" stroke="url(#solid_rim)" stroke-width="6" stroke-linejoin="round"/>
  <rect x="525" y="655" width="245" height="88" rx="22" 
        fill="url(#cursor_chrome)" stroke="url(#solid_rim)" stroke-width="6"/>
</g>
</svg>"""

SIZES = [16, 24, 32, 48, 64, 128, 256]

def build_icon(name: str, svg_content: str):
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
    print(f"[+] 生成完成: {name}.ico (尺寸: {im.size})")

if __name__ == "__main__":
    # 1.00x 标准优雅留白款作为 titanium 与 titanium_solid 的默认定义
    build_icon("titanium", get_svg_content(1.00))
    build_icon("titanium_solid", get_svg_content(1.00))
    # 额外提供 0.95x 更加从容款
    build_icon("titanium_airy", get_svg_content(0.95))
    print("WT 图标重新精修构建完毕！")
