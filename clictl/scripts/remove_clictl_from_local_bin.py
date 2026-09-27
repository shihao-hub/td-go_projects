# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# 配套 copy_clictl_to_local_bin.py：从用户级工具目录移除 clictl.exe
# 用法：uv run scripts/remove_clictl_from_local_bin.py

import sys
from pathlib import Path

DEST = Path(r"D:\Users\language_projects_bin") / "clictl.exe"

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def main() -> int:
    if DEST.exists():
        try:
            DEST.unlink()
        except OSError as exc:
            print(f"删除失败（clictl 可能正在运行）: {exc}", file=sys.stderr)
            return 1
        print(f"已删除: {DEST}")
    else:
        print(f"未找到: {DEST}（无需删除）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
