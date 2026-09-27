# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# sourcecount 构建脚本：版本号经 ldflags 注入 CLI 与 MCP 两处
# 用法：uv run scripts/build.py [--version 1.0.0]，默认 dev

import argparse
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
    parser = argparse.ArgumentParser(description="构建 sourcecount.exe（版本注入 cli 与 mcp 两处）")
    parser.add_argument("--version", default="dev", help="版本号（默认 dev）")
    args = parser.parse_args()

    BUILD_DIR.mkdir(parents=True, exist_ok=True)
    ldflags = (
        f"-X sourcecount/internal/cli.Version={args.version}"
        f" -X sourcecount/internal/mcp.Version={args.version}"
    )
    r = subprocess.run(
        ["go", "build", "-ldflags", ldflags, "-o", str(BUILD_DIR / "sourcecount.exe"), "./cmd/sourcecount"],
        cwd=str(PROJECT_ROOT),
    )
    if r.returncode != 0:
        return r.returncode
    print(f"已生成 {BUILD_DIR / 'sourcecount.exe'} ({args.version})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
