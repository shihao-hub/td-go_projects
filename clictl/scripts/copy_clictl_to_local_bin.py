# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# 把双通道构建产物安装到用户级工具目录（已入 PATH）：
#   clictl-cli[-dev].exe -> clictl.exe      （终端命令名保持 clictl）
#   clictl-gui[-dev].exe -> clictl-gui.exe  （桌面启动台，与 CLI 同通道）
# 用法：uv run scripts/copy_clictl_to_local_bin.py [--channel dev|prod]
# 说明：安装目录为 D:\Users\language_projects_bin（260922 起由 ~/.local/bin 迁移而来）

import argparse
import shutil
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = PROJECT_ROOT / "bin"
DEST_DIR = Path(r"D:\Users\language_projects_bin")

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def resolve(channel: str) -> tuple[Path, Path, str]:
    """返回 (cli 源, gui 源, 通道名)，优先用户指定通道，缺省自动选已存在的。"""
    candidates = {
        "prod": (OUT_DIR / "clictl-cli.exe", OUT_DIR / "clictl-gui.exe"),
        "dev": (OUT_DIR / "clictl-cli-dev.exe", OUT_DIR / "clictl-gui-dev.exe"),
    }
    if channel:
        cli, gui = candidates[channel]
        return cli, gui, channel
    for name in ("prod", "dev"):
        cli, gui = candidates[name]
        if cli.is_file() and gui.is_file():
            return cli, gui, name
    return candidates["dev"][0], candidates["dev"][1], "dev"


def main() -> int:
    parser = argparse.ArgumentParser(description="安装 clictl 双通道产物到用户工具目录")
    parser.add_argument("--channel", choices=["dev", "prod"], default="", help="指定通道（默认自动选择）")
    args = parser.parse_args()

    cli_src, gui_src, channel = resolve(args.channel)
    missing = [p for p in (cli_src, gui_src) if not p.is_file()]
    if missing:
        print("未找到以下产物，请先执行构建：uv run scripts/build.py", file=sys.stderr)
        for p in missing:
            print(f"  - {p}", file=sys.stderr)
        return 1

    DEST_DIR.mkdir(parents=True, exist_ok=True)
    targets = (
        (cli_src, DEST_DIR / "clictl.exe"),
        (gui_src, DEST_DIR / "clictl-gui.exe"),
    )
    for src, dest in targets:
        shutil.copy2(src, dest)
        print(f"已复制: {src.name} -> {dest}")

    print(f"通道: {channel}（运行时数据落 %APPDATA%\\language_projects\\clictl\\{channel}\\）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
