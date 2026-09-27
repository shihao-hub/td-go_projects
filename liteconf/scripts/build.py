# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# liteconf 构建脚本：构建 server 与调试 CLI 两个二进制到 <项目根>/build/
# 用法：
#   uv run scripts/build.py                  # 增量构建（版本号默认 0.1.0）
#   uv run scripts/build.py --clean          # 清空 build/ 后全量构建
#   uv run scripts/build.py --version 0.2.0  # 指定版本号，注入两个二进制

import argparse
import shutil
import subprocess
import sys
from pathlib import Path

# 项目根 = 本脚本所在 scripts/ 的上一级，不依赖调用位置
PROJECT_ROOT = Path(__file__).resolve().parent.parent
BUILD_DIR = PROJECT_ROOT / "build"
SERVER_OUTPUT = BUILD_DIR / "liteconf-server.exe"
CLI_OUTPUT = BUILD_DIR / "liteconf.exe"

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def main() -> int:
    parser = argparse.ArgumentParser(description="构建 liteconf server 与调试 CLI 两个二进制到 build/")
    parser.add_argument("--clean", action="store_true", help="清空 build/ 后全量构建")
    parser.add_argument("--version", default="0.1.0", help="版本号（默认 0.1.0）")
    args = parser.parse_args()

    if args.clean and BUILD_DIR.exists():
        shutil.rmtree(BUILD_DIR)
        print(f"cleaned: {BUILD_DIR}")

    BUILD_DIR.mkdir(parents=True, exist_ok=True)

    # 版本号经 internal/version.Version 注入，两个二进制使用同一条 ldflags（注入方式一致）
    # -trimpath 去除本机路径；-s -w 裁剪符号表与调试信息减小体积
    ldflags = f"-s -w -X github.com/shihao-hub/liteconf/internal/version.Version={args.version}"

    for output, pkg in ((SERVER_OUTPUT, "./cmd/liteconf-server"), (CLI_OUTPUT, "./cmd/liteconf")):
        r = subprocess.run(
            ["go", "build", "-trimpath", "-ldflags", ldflags, "-o", str(output), pkg],
            cwd=str(PROJECT_ROOT),
        )
        if r.returncode != 0:
            print(f"go build failed (exit {r.returncode}): {pkg}", file=sys.stderr)
            return r.returncode

    for output in (SERVER_OUTPUT, CLI_OUTPUT):
        if not output.is_file():
            print(f"build finished but output not found: {output}", file=sys.stderr)
            return 1

    for output in (SERVER_OUTPUT, CLI_OUTPUT):
        print(f"build ok: {output} ({output.stat().st_size / 1024 / 1024:.2f} MB)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
