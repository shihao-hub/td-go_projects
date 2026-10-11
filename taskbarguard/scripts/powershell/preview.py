# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
from pathlib import Path
from PIL import Image

custom_dir = Path(__file__).resolve().parent / "custom_icons"
dsh_dir = Path(r"D:\Users\language_projects\go_projects\taskbarguard\scripts\deepseek_harness\custom_icons")
out_file = custom_dir / "comparison_preview.png"

# 加载生成的 5 款款式
im_underlit = Image.open(custom_dir / "underlit_chrome.png").resize((96, 96), Image.LANCZOS)
im_blue = Image.open(custom_dir / "dark_blue.png").resize((96, 96), Image.LANCZOS)
im_titanium = Image.open(custom_dir / "titanium.png").resize((96, 96), Image.LANCZOS)
im_cyan = Image.open(custom_dir / "cyan_matrix.png").resize((96, 96), Image.LANCZOS)
im_dark = Image.open(custom_dir / "pure_dark.png").resize((96, 96), Image.LANCZOS)

# 参考图标 (DeepSeek 8_underlit_chrome)
im_ds = Image.open(dsh_dir / "8_underlit_chrome.png").resize((96, 96), Image.LANCZOS)

# 模拟 Win11 浅天蓝磨砂任务栏 (与用户截图背景色保持一致)
bg_color = (205, 230, 245, 255)
width = 120 + 6 * 140
height = 160
strip = Image.new("RGBA", (width, height), bg_color)

# 排列: DeepSeek参考 -> 1.underlit_chrome (首选) -> 2.dark_blue -> 3.titanium -> 4.cyan_matrix -> 5.pure_dark
items = [
    (im_ds, "DeepSeek(Ref)"),
    (im_underlit, "Underlit⭐"),
    (im_blue, "DarkBlue⭐"),
    (im_titanium, "Titanium"),
    (im_cyan, "CyanMatrix"),
    (im_dark, "PureDark"),
]

for idx, (img, label) in enumerate(items):
    x = 40 + idx * 140
    y = 32
    strip.paste(img, (x, y), img)

strip.save(out_file)
print(f"[+] 任务栏对比预览图已生成: {out_file}")
