# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# 构建单文件 clictl.exe；版本号唯一来源 = -ldflags -X 注入
# 用法：uv run scripts/build.py [--version 1.0.0]，默认 dev

import argparse
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def main() -> int:
    parser = argparse.ArgumentParser(description="构建单文件 clictl.exe")
    parser.add_argument("--version", default="dev", help="版本号（默认 dev）")
    args = parser.parse_args()

    r = subprocess.run(
        [
            "go", "build", "-trimpath",
            "-ldflags", f"-s -w -X clictl/internal/cli.Version={args.version}",
            "-o", "clictl.exe", "./cmd/clictl",
        ],
        cwd=str(PROJECT_ROOT),
    )
    if r.returncode != 0:
        print(f"go build 失败（exit {r.returncode}）", file=sys.stderr)
        return r.returncode
    print(f"构建完成: {PROJECT_ROOT / 'clictl.exe'} (v{args.version})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
