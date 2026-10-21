# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# 将 agyquota 双通道产物部署到用户级工具目录（已入 PATH）。
# 用法：uv run scripts/install.py [--release]
# 说明：安装目录为 D:\Users\language_projects_bin；未构建时自动先跑 build.py。
# Console 产物安装为 agyquota-cli.exe，GUI 产物安装为 agyquota-gui.exe；
# 双通道拆分前的 agyquota.exe 会被移除，避免终端继续命中旧单产物。

import shutil
import subprocess
import sys
from pathlib import Path

SCRIPTS_DIR = Path(__file__).resolve().parent
PROJECT_ROOT = SCRIPTS_DIR.parent
TARGET_DIR = Path(r"D:\Users\language_projects_bin")

PAIRS = (
    ("agyquota-gui.exe", "agyquota-gui.exe"),
    ("agyquota-cli.exe", "agyquota-cli.exe"),
)
LEGACY_NAMES = ("agyquota.exe", "agyquota-dev.exe")

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def build() -> int:
    # PEP 723 脚本自身没有 pillow，交给 uv run 解析依赖。
    return subprocess.run(["uv", "run", str(SCRIPTS_DIR / "build.py")], cwd=str(PROJECT_ROOT)).returncode


def main() -> int:
    missing = [name for name, _ in PAIRS if not (PROJECT_ROOT / name).is_file()]
    if missing:
        print(f"未找到 {', '.join(missing)}，先执行构建...")
        rc = build()
        if rc != 0:
            return rc
        missing = [name for name, _ in PAIRS if not (PROJECT_ROOT / name).is_file()]
        if missing:
            print(f"构建后仍未找到: {', '.join(missing)}", file=sys.stderr)
            return 1

    if not TARGET_DIR.is_dir():
        print(f"未找到目标安装目录: {TARGET_DIR}", file=sys.stderr)
        return 1

    for source_name, target_name in PAIRS:
        shutil.copy2(PROJECT_ROOT / source_name, TARGET_DIR / target_name)
        print(f"已安装: {TARGET_DIR / target_name}")

    for legacy_name in LEGACY_NAMES:
        legacy = TARGET_DIR / legacy_name
        if legacy.is_file():
            legacy.unlink()
            print(f"已移除历史单产物: {legacy}")

    print("提示：终端请使用 agyquota-cli，桌面面板请使用 agyquota-gui。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
