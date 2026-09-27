# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# pythonlauncher 构建脚本：在项目根产出 launcher.exe
# 用法：uv run build.py（在 pythonlauncher 项目根执行）

import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent
OUTPUT = PROJECT_ROOT / "launcher.exe"

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def main() -> int:
    r = subprocess.run(
        ["go", "build", "-trimpath", "-ldflags", "-s -w", "-o", str(OUTPUT), "."],
        cwd=str(PROJECT_ROOT),
    )
    if r.returncode != 0:
        return r.returncode
    print(f"built: {OUTPUT}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
