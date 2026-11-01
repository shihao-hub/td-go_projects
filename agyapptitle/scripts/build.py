# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""agyapptitle 双通道构建流水线。

产物规则（严格遵循 Monorepo dev 与 release 隔离约束）：
  - dev（默认）：agyapptitle-dev.exe（CLI 全能版）+ agyapptitlew-dev.exe（无黑框后台版）
    数据目录绑定：%APPDATA%\\language_projects\\agyapptitle\\dev
    开机自启动：严格禁止写入注册表（强校验拦截）
  - release（--release）：agyapptitle.exe（CLI 全能版）+ agyapptitlew.exe（无黑框后台版）
    数据目录绑定：%APPDATA%\\language_projects\\agyapptitle\\prod
    开机自启动：允许向注册表注册自启动

用法：
  uv run scripts/build.py            # 构建 dev 开发版（默认）
  uv run scripts/build.py --release  # 构建 release 正式版
"""

from __future__ import annotations

import argparse
import datetime
import os
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
BIN_DIR = PROJECT_ROOT / "bin"
MODULE_PATH = "language_projects/go_projects/agyapptitle"


def get_git_info() -> tuple[str, bool]:
    commit = "unknown"
    is_dirty = False
    try:
        r = subprocess.run(["git", "rev-parse", "--short", "HEAD"], capture_output=True, text=True, cwd=PROJECT_ROOT)
        if r.returncode == 0:
            commit = r.stdout.strip()
        diff = subprocess.run(["git", "status", "--porcelain"], capture_output=True, text=True, cwd=PROJECT_ROOT)
        if diff.returncode == 0 and diff.stdout.strip():
            is_dirty = True
    except Exception:
        pass
    return commit, is_dirty


def main():
    parser = argparse.ArgumentParser(description="agyapptitle 构建脚本")
    parser.add_argument("--release", action="store_true", help="构建 release 正式发行版（默认构建 dev 开发版）")
    parser.add_argument("--version", default="v1.0.0", help="版本号（默认 v1.0.0）")
    args = parser.parse_args()

    BIN_DIR.mkdir(parents=True, exist_ok=True)
    commit, is_dirty = get_git_info()
    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")

    mode = "release" if args.release else "dev"
    raw_version = args.version.lstrip("v")

    if mode == "release":
        version = f"v{raw_version}"
        build_id = f"{version}.{commit}"
        cli_name = "agyapptitle.exe"
        daemon_name = "agyapptitlew.exe"
    else:
        dirty_suffix = "-dirty" if is_dirty else ""
        version = f"v{raw_version}-dev{dirty_suffix}"
        build_id = f"{commit}{dirty_suffix}.{timestamp}"
        cli_name = "agyapptitle-dev.exe"
        daemon_name = "agyapptitlew-dev.exe"

    ldflags = (
        f"-s -w "
        f"-X '{MODULE_PATH}/internal/buildinfo.Version={version}' "
        f"-X '{MODULE_PATH}/internal/buildinfo.BuildID={build_id}' "
        f"-X '{MODULE_PATH}/internal/buildinfo.BuildMode={mode}'"
    )

    print(f"\n========================================================")
    print(f"[*] 开始构建 agyapptitle [{mode.upper()} 模式]")
    print(f"[*] Version:   {version}")
    print(f"[*] BuildID:   {build_id}")
    print(f"[*] 产物目录:  {BIN_DIR}")
    print(f"========================================================\n")

    # 1. 编译控制台 CLI 版
    cli_out = BIN_DIR / cli_name
    print(f"[*] 正在构建 CLI 全能版: {cli_name} ...")
    cmd_cli = [
        "go",
        "build",
        f"-ldflags={ldflags}",
        "-o",
        str(cli_out),
        "./cmd/agyapptitle",
    ]
    res_cli = subprocess.run(cmd_cli, cwd=PROJECT_ROOT)
    if res_cli.returncode != 0:
        print("[!] CLI 构建失败", file=sys.stderr)
        sys.exit(res_cli.returncode)
    print(f"[+] CLI 构建成功 -> {cli_out}")

    # 2. 编译无黑框后台守护版 (-H=windowsgui)
    daemon_out = BIN_DIR / daemon_name
    print(f"[*] 正在构建无黑框守护版: {daemon_name} ...")
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

    print(f"\n[OK] agyapptitle [{mode.upper()}] 双二进制构建全部完成！\n")


if __name__ == "__main__":
    main()
