# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""agyapptitle 构建流水线脚本。

编译输出控制台全能版 (agyapptitle.exe) 与静默无黑框后台版 (agyapptitlew.exe)。
"""

import datetime
import os
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
BIN_DIR = PROJECT_ROOT / "bin"


def get_git_info() -> tuple[str, str]:
    commit = "unknown"
    try:
        r = subprocess.run(["git", "rev-parse", "--short", "HEAD"], capture_output=True, text=True, cwd=PROJECT_ROOT)
        if r.returncode == 0:
            commit = r.stdout.strip()
    except Exception:
        pass
    date_str = datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    return commit, date_str


def main():
    BIN_DIR.mkdir(parents=True, exist_ok=True)
    commit, date_str = get_git_info()
    version = "1.0.0"

    ldflags = (
        f"-s -w "
        f"-X 'main.version={version}' "
        f"-X 'main.commit={commit}' "
        f"-X 'main.date={date_str}'"
    )

    # 1. 编译全能控制台版 agyapptitle.exe
    cli_out = BIN_DIR / "agyapptitle.exe"
    print(f"[*] 正在构建全能 CLI 版: {cli_out} ...")
    cmd_cli = [
        "go",
        "build",
        f"-ldflags={ldflags}",
        "-o",
        str(cli_out),
        "./cmd/agyapptitle",
    ]
    res = subprocess.run(cmd_cli, cwd=PROJECT_ROOT)
    if res.returncode != 0:
        print("[!] CLI 构建失败", file=sys.stderr)
        sys.exit(res.returncode)
    print(f"[+] CLI 构建成功 -> {cli_out}")

    # 2. 编译静默后台无黑框版 agyapptitlew.exe (-H=windowsgui)
    daemon_out = BIN_DIR / "agyapptitlew.exe"
    print(f"[*] 正在构建无黑框守护进程版: {daemon_out} ...")
    cmd_daemon = [
        "go",
        "build",
        f"-ldflags={ldflags} -H=windowsgui",
        "-o",
        str(daemon_out),
        "./cmd/agyapptitle",
    ]
    res_daemon = subprocess.run(cmd_daemon, cwd=PROJECT_ROOT)
    if res_daemon.returncode != 0:
        print("[!] 后台版构建失败", file=sys.stderr)
        sys.exit(res_daemon.returncode)
    print(f"[+] 后台版构建成功 -> {daemon_out}")

    print("\n[OK] agyapptitle 全部二进制构建完成！")


if __name__ == "__main__":
    main()
