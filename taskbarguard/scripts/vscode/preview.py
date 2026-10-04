from pathlib import Path
from PIL import Image

vscode_dir = Path(r"D:\Users\language_projects\.scripts\vscode")
deepseek_icon = Path(r"D:\Users\language_projects\.scripts\custom_icons\3_dark_monochrome_centered.png")

blue_png = vscode_dir / "vscode_dark_blue.png"
silver_png = vscode_dir / "vscode_dark_silver.png"

# Load images
im_blue = Image.open(blue_png).resize((128, 128), Image.LANCZOS)
im_silver = Image.open(silver_png).resize((128, 128), Image.LANCZOS)
im_ds = Image.open(deepseek_icon).resize((128, 128), Image.LANCZOS)

# Create a strip preview on taskbar background (simulate light blue / light gray taskbar background like user screenshot)
bg_color = (205, 230, 245, 255) # taskbar light sky blue
preview = Image.new("RGBA", (550, 160), bg_color)

# Paste: DeepSeek, VS Code Blue, VS Code Silver
preview.paste(im_ds, (40, 16), im_ds)
preview.paste(im_blue, (210, 16), im_blue)
preview.paste(im_silver, (380, 16), im_silver)

out_preview = vscode_dir / "preview_comparison.png"
preview.save(out_preview)
print(f"[+] 预览图生成成功: {out_preview}")
