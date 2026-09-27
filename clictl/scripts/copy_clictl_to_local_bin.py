# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# 把构建产物 clictl.exe 复制到用户级工具目录（已入 PATH）
# 用法：uv run scripts/copy_clictl_to_local_bin.py
# 说明：安装目录为 D:\Users\language_projects_bin（260922 起由 ~/.local/bin 迁移而来）

import shutil
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
DEST_DIR = Path(r"D:\Users\language_projects_bin")

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def main() -> int:
    src = PROJECT_ROOT / "clictl.exe"
    if not src.is_file():
        print(f"未找到 {src}，请先执行构建：uv run scripts/build.py", file=sys.stderr)
        return 1
    DEST_DIR.mkdir(parents=True, exist_ok=True)
    dest = DEST_DIR / "clictl.exe"
    shutil.copy2(src, dest)
    print(f"已复制: {src} -> {dest}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
