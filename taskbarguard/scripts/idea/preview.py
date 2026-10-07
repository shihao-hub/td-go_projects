from pathlib import Path
from PIL import Image

idea_custom_dir = Path(__file__).resolve().parent / "custom_icons"
vsc_dir = Path(r"D:\Users\language_projects\go_projects\taskbarguard\scripts\vscode")
dsh_dir = Path(r"D:\Users\language_projects\go_projects\taskbarguard\scripts\deepseek_harness\custom_icons")

# Load images
im_brand = Image.open(idea_custom_dir / "brand.png").resize((128, 128), Image.LANCZOS)
im_silver = Image.open(idea_custom_dir / "silver.png").resize((128, 128), Image.LANCZOS)
im_blue = Image.open(idea_custom_dir / "dark_blue.png").resize((128, 128), Image.LANCZOS)

# Context reference icons
im_ds = Image.open(dsh_dir / "8_underlit_chrome.png").resize((128, 128), Image.LANCZOS)
im_vsc = Image.open(vsc_dir / "vscode_dark_blue.png").resize((128, 128), Image.LANCZOS)

# Windows 11 Taskbar simulated sky blue background (same as user's screenshot)
bg_color = (205, 230, 245, 255)
strip = Image.new("RGBA", (880, 160), bg_color)

# Paste: DeepSeek, VS Code, IDEA Brand, IDEA Silver, IDEA Dark Blue
strip.paste(im_ds, (30, 16), im_ds)
strip.paste(im_vsc, (200, 16), im_vsc)
strip.paste(im_brand, (370, 16), im_brand)
strip.paste(im_silver, (540, 16), im_silver)
strip.paste(im_blue, (710, 16), im_blue)

out_preview = idea_custom_dir / "comparison_preview.png"
strip.save(out_preview)
print(f"[+] 任务栏对比预览图已生成: {out_preview}")
