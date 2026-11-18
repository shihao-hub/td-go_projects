# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""ccquota 专属图标渲染：透明背景 + 深色面板 + 三档额度条。

产物：
  - build/appicon.png        1024×1024 原图（透明底）
  - icon.ico                 多尺寸 ico，供 go-winres 打进 PE 资源
  - cmd/ccquota-gui/icon.ico go:embed 的托盘图标（与上者同源）

同一源图重复执行必须产出相同字节，避免每次构建都把仓库弄脏。
"""

from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parent.parent
SIZE = 1024

# 配色与 WebUI 保持一致
PANEL = (26, 27, 32, 255)
PANEL_EDGE = (255, 122, 47, 255)
BAR_OK = (74, 222, 128, 255)
BAR_WARN = (251, 191, 36, 255)
TRACK = (47, 49, 58, 255)

ICO_SIZES = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]


def render(size: int = SIZE) -> Image.Image:
    im = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)

    # 圆角面板 + 橙色描边（透明背景，不留白底）
    d.rounded_rectangle((64, 64, size - 64, size - 64), radius=180, fill=PANEL, outline=PANEL_EDGE, width=26)

    # 三档额度条：两档充足、一档告警，长度依次递减
    left, right = 190, size - 190
    bars = [
        (0.32, BAR_OK),
        (0.52, BAR_OK),
        (0.72, BAR_WARN),
    ]
    height = 74
    gap = 86
    for row, (ratio, color) in enumerate(bars):
        top = 300 + row * (height + gap)
        d.rounded_rectangle((left, top, right, top + height), radius=height // 2, fill=TRACK)
        d.rounded_rectangle((left, top, left + int((right - left) * ratio), top + height), radius=height // 2, fill=color)

    return im


def main() -> int:
    build_dir = ROOT / "build"
    build_dir.mkdir(parents=True, exist_ok=True)

    master = render(SIZE)
    master.save(build_dir / "appicon.png", format="PNG")

    ico_root = ROOT / "icon.ico"
    master.save(ico_root, format="ICO", sizes=ICO_SIZES)

    gui_icon = ROOT / "cmd" / "ccquota-gui" / "icon.ico"
    gui_icon.parent.mkdir(parents=True, exist_ok=True)
    gui_icon.write_bytes(ico_root.read_bytes())

    print(f"渲染完成: {ico_root.name} / {gui_icon.relative_to(ROOT)} / build/appicon.png")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
