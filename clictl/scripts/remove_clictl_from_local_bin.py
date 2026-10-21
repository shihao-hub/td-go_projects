# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# 配套 copy_clictl_to_local_bin.py：从用户级工具目录移除 clictl 双通道产物
# 用法：uv run scripts/remove_clictl_from_local_bin.py

import sys
from pathlib import Path

DEST_DIR = Path(r"D:\Users\language_projects_bin")
TARGETS = (DEST_DIR / "clictl.exe", DEST_DIR / "clictl-gui.exe")

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def main() -> int:
    failed = 0
    for dest in TARGETS:
        if not dest.exists():
            print(f"未找到: {dest}（无需删除）")
            continue
        try:
            dest.unlink()
        except OSError as exc:
            print(f"删除失败（是否仍在运行？）: {dest}: {exc}", file=sys.stderr)
            failed += 1
            continue
        print(f"已删除: {dest}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
