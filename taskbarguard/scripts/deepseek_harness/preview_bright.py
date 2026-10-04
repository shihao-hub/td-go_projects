from pathlib import Path
from PIL import Image

dsh_dir = Path(r"D:\Users\language_projects\go_projects\taskbarguard\scripts\deepseek_harness\custom_icons")
vsc_dir = Path(r"D:\Users\language_projects\go_projects\taskbarguard\scripts\vscode")

# 对比：款式5(原钛金属)、款式7(白金亮钛)、款式8(底光通透)、VS Code银白
im_5 = Image.open(dsh_dir / "5_titanium_sculpted.png").resize((128, 128), Image.LANCZOS)
im_7 = Image.open(dsh_dir / "7_bright_titanium.png").resize((128, 128), Image.LANCZOS)
im_8 = Image.open(dsh_dir / "8_underlit_chrome.png").resize((128, 128), Image.LANCZOS)
im_vsc = Image.open(vsc_dir / "vscode_dark_silver.png").resize((128, 128), Image.LANCZOS)

bg_color = (205, 230, 245, 255)
strip = Image.new("RGBA", (660, 160), bg_color)

strip.paste(im_5, (20, 16), im_5)
strip.paste(im_7, (180, 16), im_7)
strip.paste(im_8, (340, 16), im_8)
strip.paste(im_vsc, (500, 16), im_vsc)

out_preview = dsh_dir / "bright_comparison_preview.png"
strip.save(out_preview)
print(f"[+] 提亮对比图已生成: {out_preview}")
