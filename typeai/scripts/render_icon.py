# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""渲染 typeai 专属图标：深色工作台底 + 琥珀色对话气泡与提示符光标。

输出：
  - icon.png                 1024x1024 原图
  - icon.ico                 多尺寸 Windows 图标（release / PE 资源）
  - internal/app/icon.ico    go:embed 运行时图标（release）
"""

from pathlib import Path

from PIL import Image, ImageDraw

SIZE = 1024
BG = (23, 24, 26, 255)
PANEL = (30, 32, 35, 255)
ACCENT = (217, 164, 65, 255)
ACCENT_DIM = (217, 164, 65, 150)


def render_icon(size: int = SIZE) -> Image.Image:
    image = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)

    margin = int(size * 0.055)
    radius = int(size * 0.19)
    draw.rounded_rectangle(
        (margin, margin, size - margin, size - margin),
        radius=radius,
        fill=BG,
        outline=ACCENT,
        width=max(6, int(size * 0.011)),
    )

    # 内层工作台面板，增加层次但不使用渐变。
    inset = int(size * 0.135)
    draw.rounded_rectangle(
        (inset, inset, size - inset, size - inset),
        radius=int(radius * 0.62),
        fill=PANEL,
        outline=ACCENT_DIM,
        width=max(3, int(size * 0.005)),
    )

    # 对话气泡：圆角框 + 左下小尾巴。
    left, top, right, bottom = int(size * 0.25), int(size * 0.28), int(size * 0.75), int(size * 0.66)
    draw.rounded_rectangle(
        (left, top, right, bottom),
        radius=int(size * 0.09),
        outline=ACCENT,
        width=max(10, int(size * 0.024)),
    )
    draw.polygon(
        [
            (int(size * 0.33), bottom - 4),
            (int(size * 0.30), int(size * 0.75)),
            (int(size * 0.42), bottom - 4),
        ],
        fill=BG,
    )
    draw.line(
        [
            (int(size * 0.33), bottom - 4),
            (int(size * 0.30), int(size * 0.75)),
            (int(size * 0.42), bottom - 4),
        ],
        fill=ACCENT,
        width=max(10, int(size * 0.024)),
        joint="curve",
    )

    # 提示符 > 与光标块。
    stroke = max(14, int(size * 0.034))
    draw.line(
        [
            (int(size * 0.35), int(size * 0.38)),
            (int(size * 0.47), int(size * 0.47)),
            (int(size * 0.35), int(size * 0.56)),
        ],
        fill=ACCENT,
        width=stroke,
        joint="curve",
    )
    draw.rounded_rectangle(
        (int(size * 0.53), int(size * 0.505), int(size * 0.66), int(size * 0.555)),
        radius=max(3, int(size * 0.008)),
        fill=ACCENT,
    )
    return image


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    image = render_icon(SIZE)
    image.save(root / "icon.png")

    sizes = [256, 128, 64, 48, 32, 24, 16]
    image.save(root / "icon.ico", format="ICO", sizes=[(s, s) for s in sizes])
    (root / "internal" / "app").mkdir(parents=True, exist_ok=True)
    image.save(root / "internal" / "app" / "icon.ico", format="ICO", sizes=[(s, s) for s in sizes])
    print("[OK] typeai 图标生成完成")


if __name__ == "__main__":
    main()
