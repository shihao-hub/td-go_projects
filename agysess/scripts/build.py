# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

import os
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
BIN_DIR = PROJECT_ROOT / "bin"
MINGW_BIN = Path(r"D:\msys64\mingw64\bin")


def main():
    BIN_DIR.mkdir(parents=True, exist_ok=True)

    # 注入 mingw64 到 PATH
    env = os.environ.copy()
    if MINGW_BIN.exists():
        env["PATH"] = str(MINGW_BIN) + os.pathsep + env.get("PATH", "")

    # 1. 编译全能控制台版 agysess.exe (包含 CLI 全套命令与 gui 命令)
    cli_out = BIN_DIR / "agysess.exe"
    print(f"[*] 正在构建 CLI 全能版: {cli_out} ...")
    cmd_cli = [
        "go",
        "build",
        "-ldflags=-s -w",
        "-o",
        str(cli_out),
        "./cmd/agysess",
    ]
    res = subprocess.run(cmd_cli, cwd=PROJECT_ROOT, env=env)
    if res.returncode != 0:
        print("[!] CLI 构建失败", file=sys.stderr)
        sys.exit(res.returncode)
    print(f"[+] CLI 构建成功 -> {cli_out}")

    # 2. 编译静默桌面窗口版 agysess-gui.exe (带 -H=windowsgui，双击无黑框)
    gui_out = BIN_DIR / "agysess-gui.exe"
    print(f"[*] 正在构建无黑框 GUI 桌面版: {gui_out} ...")
    cmd_gui = [
        "go",
        "build",
        "-ldflags=-s -w -H=windowsgui",
        "-o",
        str(gui_out),
        "./cmd/agysess",
    ]
    res_gui = subprocess.run(cmd_gui, cwd=PROJECT_ROOT, env=env)
    if res_gui.returncode != 0:
        print("[!] GUI 构建失败", file=sys.stderr)
        sys.exit(res_gui.returncode)
    print(f"[+] GUI 桌面版构建成功 -> {gui_out}")

    print("\n[OK] 全部产物构建完成！")


if __name__ == "__main__":
    main()
