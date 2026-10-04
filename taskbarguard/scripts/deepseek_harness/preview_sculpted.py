from pathlib import Path
from PIL import Image

dsh_dir = Path(r"D:\Users\language_projects\go_projects\taskbarguard\scripts\deepseek_harness\custom_icons")
vsc_dir = Path(r"D:\Users\language_projects\go_projects\taskbarguard\scripts\vscode")

# 4个图标：旧款款式3、新款5钛金属、新款6丝绸微浮雕、VS Code银白
old_3 = Image.open(dsh_dir / "3_dark_monochrome_centered.png").resize((128, 128), Image.LANCZOS)
new_5 = Image.open(dsh_dir / "5_titanium_sculpted.png").resize((128, 128), Image.LANCZOS)
new_6 = Image.open(dsh_dir / "6_luminous_relief.png").resize((128, 128), Image.LANCZOS)
vsc = Image.open(vsc_dir / "vscode_dark_silver.png").resize((128, 128), Image.LANCZOS)

# 创建并排预览底图 (天蓝色任务栏背景)
bg_color = (205, 230, 245, 255)
strip = Image.new("RGBA", (660, 160), bg_color)

strip.paste(old_3, (20, 16), old_3)
strip.paste(new_5, (180, 16), new_5)
strip.paste(new_6, (340, 16), new_6)
strip.paste(vsc, (500, 16), vsc)

out_preview = dsh_dir / "sculpted_comparison_preview.png"
strip.save(out_preview)
print(f"[+] 新版对比图已生成: {out_preview}")
