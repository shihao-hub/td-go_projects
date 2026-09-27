# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# 构建托盘版（GUI，隐藏控制台窗口）
# 用法：uv run scripts/build-tray.py（可从任意目录执行）

import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
BUILD_DIR = PROJECT_ROOT / "build"

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def main() -> int:
    BUILD_DIR.mkdir(parents=True, exist_ok=True)
    r = subprocess.run(
        [
            "go", "build", "-trimpath",
            "-ldflags", "-s -w -H windowsgui",
            "-o", str(BUILD_DIR / "sublimefolders.exe"),
            "./cmd/sublimefolders",
        ],
        cwd=str(PROJECT_ROOT),
    )
    return r.returncode


if __name__ == "__main__":
    sys.exit(main())
