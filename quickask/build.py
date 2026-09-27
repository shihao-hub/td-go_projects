# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# quickask 构建脚本：CLI 客户端 + 后端守护两个控制台程序（fyne GUI 已弃）
# 用法：uv run build.py（在 quickask 项目根执行）

import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent
BIN_DIR = PROJECT_ROOT / "bin"

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def main() -> int:
    BIN_DIR.mkdir(parents=True, exist_ok=True)

    for out, pkg in (("quickaskd.exe", "./cmd/quickaskd"), ("quickask.exe", "./cmd/quickask")):
        r = subprocess.run(["go", "build", "-o", str(BIN_DIR / out), pkg], cwd=str(PROJECT_ROOT))
        if r.returncode != 0:
            print(f"go build 失败（exit {r.returncode}）: {pkg}", file=sys.stderr)
            return r.returncode

    print("build OK: bin\\quickask.exe + bin\\quickaskd.exe")
    return 0


if __name__ == "__main__":
    sys.exit(main())
