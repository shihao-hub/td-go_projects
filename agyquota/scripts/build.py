# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# 构建单文件 agyquota.exe；版本与 buildID 均由 git describe 注入。
# 用法：uv run scripts/build.py [--release]
#   --release: 仅允许干净 tag 检出（describe 形如 v1.2.3 或 agyquota/v1.2.3）时构建

import argparse
import re
import subprocess
import sys
import time
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def main() -> int:
    parser = argparse.ArgumentParser(description="构建单文件 agyquota.exe（git describe 注入版本与 buildID）")
    parser.add_argument("--release", action="store_true",
                        help="仅允许干净 tag 检出（describe 形如 v1.2.3 或 agyquota/v1.2.3）时构建")
    args = parser.parse_args()

    r = subprocess.run(
        ["git", "describe", "--tags", "--always", "--dirty"],
        cwd=str(PROJECT_ROOT), capture_output=True, text=True, encoding="utf-8", errors="replace",
    )
    if r.returncode != 0:
        print(f"git describe 失败（exit {r.returncode}）", file=sys.stderr)
        return r.returncode
    describe = r.stdout.strip()

    if args.release and ("-dirty" in describe
                         or not re.match(r"^(?:agyquota/)?v\d+\.\d+\.\d+$", describe)):
        print(f"-Release 要求干净 tag 检出（当前 describe: {describe}）", file=sys.stderr)
        return 1

    # buildID = describe + Unix 秒时间戳：区分 dirty 状态下的多次构建（v2 标准 §3.3）
    build_id = f"{describe}.{int(time.time())}"

    print(f"正在构建 agyquota.exe ({describe}, buildID={build_id})...")
    r = subprocess.run(
        [
            "go", "build", "-trimpath",
            "-ldflags",
            f"-s -w -X agyquota/internal/buildinfo.Version={describe}"
            f" -X agyquota/internal/buildinfo.BuildID={build_id}",
            "-o", "agyquota.exe", "./cmd/agyquota",
        ],
        cwd=str(PROJECT_ROOT),
    )
    if r.returncode != 0:
        print(f"go build 失败（exit {r.returncode}）", file=sys.stderr)
        return r.returncode
    print(f"构建完成: {PROJECT_ROOT / 'agyquota.exe'} ({describe})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
