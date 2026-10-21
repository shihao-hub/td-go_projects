# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""liteconf 专属图标渲染：深色配置面板 + 三行键值 + 窗口控制点。

输出：
  - icon.png           1024×1024 原图
  - icon.ico           完整 Windows 尺寸链（PE 资源与窗口图标共用）
  - internal/app/icon.ico  go:embed 的 release 运行时图标

绘制保持确定性：同一源码重复执行必须产生相同字节，否则每次构建都会
弄脏工作区，让 release 门禁自锁。
"""

from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parent.parent
SIZE = 1024

PANEL = (22, 24, 31, 255)
PANEL_EDGE = (61, 220, 151, 255)
DIVIDER = (42, 46, 58, 255)
ACCENT = (61, 220, 151, 255)
MUTED = (139, 147, 167, 255)
DOT_RED = (255, 95, 87, 255)
DOT_YELLOW = (254, 188, 46, 255)
DOT_GREEN = (40, 200, 64, 255)


def render(size: int = SIZE) -> Image.Image:
    im = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)

    # 主体面板：深色圆角 + 薄荷色描边
    d.rounded_rectangle((96, 96, 928, 928), radius=168, fill=PANEL, outline=PANEL_EDGE, width=20)

    # 顶部窗口控制点
    for cx, colour in ((212, DOT_RED), (282, DOT_YELLOW), (352, DOT_GREEN)):
        d.ellipse((cx - 22, 178, cx + 22, 222), fill=colour)

    # 标题分隔线
    d.rounded_rectangle((164, 286, 860, 294), radius=4, fill=DIVIDER)

    # 三行配置项：左侧圆点 + 右侧键值条（长度不同，形成配置列表的节奏）
    rows = (
        (398, 604, ACCENT),
        (548, 792, MUTED),
        (698, 700, MUTED),
    )
    for cy, bar_right, colour in rows:
        d.ellipse((188, cy - 26, 240, cy + 26), fill=ACCENT)
        d.rounded_rectangle((292, cy - 24, bar_right, cy + 24), radius=24, fill=colour)

    # 底部状态条：更短的强调色，暗示版本/健康状态
    d.rounded_rectangle((164, 812, 420, 832), radius=10, fill=ACCENT)

    return im


def main() -> None:
    im = render()
    im.save(ROOT / "icon.png")

    sizes = [256, 128, 64, 48, 32, 24, 16]
    frames = [(size, size) for size in sizes]
    im.save(ROOT / "icon.ico", format="ICO", sizes=frames)

    app_dir = ROOT / "internal" / "app"
    app_dir.mkdir(parents=True, exist_ok=True)
    im.save(app_dir / "icon.ico", format="ICO", sizes=frames)
    print(f"[OK] 图标已生成: {ROOT / 'icon.ico'} (+ icon.png, internal/app/icon.ico)")


if __name__ == "__main__":
    main()
