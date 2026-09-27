# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# 将 agyquota.exe 部署到用户级工具目录（已入 PATH）
# 用法：uv run scripts/install.py
# 说明：安装目录为 D:\Users\language_projects_bin；未构建时自动先跑 build.py

import shutil
import subprocess
import sys
from pathlib import Path

SCRIPTS_DIR = Path(__file__).resolve().parent
PROJECT_ROOT = SCRIPTS_DIR.parent
EXE_PATH = PROJECT_ROOT / "agyquota.exe"
TARGET_DIR = Path(r"D:\Users\language_projects_bin")

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def main() -> int:
    if not EXE_PATH.is_file():
        print("未找到 agyquota.exe，先执行构建...")
        r = subprocess.run(["uv", "run", str(SCRIPTS_DIR / "build.py")], cwd=str(PROJECT_ROOT))
        if r.returncode != 0:
            return r.returncode
        if not EXE_PATH.is_file():
            print(f"构建后仍未找到 {EXE_PATH}", file=sys.stderr)
            return 1

    if not TARGET_DIR.is_dir():
        print(f"未找到目标安装目录: {TARGET_DIR}", file=sys.stderr)
        return 1

    shutil.copy2(EXE_PATH, TARGET_DIR / "agyquota.exe")
    print(f"已将 agyquota.exe 安装到: {TARGET_DIR / 'agyquota.exe'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
