# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""agyapptitle 双通道二进制构建流水线。

产物规则（严格遵循 Monorepo dev 与 release 隔离约束）：
  - dev（默认）：agyapptitle-dev.exe（CLI 全能版）+ agyapptitlew-dev.exe（无黑框后台版）
    数据目录绑定：%APPDATA%\\language_projects\\agyapptitle\\dev
    开机自启动：严格禁止写入注册表（强校验拦截）
  - release：agyapptitle.exe（CLI 全能版）+ agyapptitlew.exe（无黑框后台版）
    数据目录绑定：%APPDATA%\\language_projects\\agyapptitle\\prod
    开机自启动：允许向注册表注册自启动
    约束门禁：仅当 HEAD 严格命中 agyapptitle/v* tag 且工作区干净时允许产出 release；
    未满足条件却显式传 --release 时脚本直接报错退出（Exit Code 2）。

用法：
  uv run scripts/build.py            # 构建 dev 开发版（默认）
  uv run scripts/build.py --release  # 发布构建（条件不满足时拒绝并报错退出）
  uv run scripts/build.py --version 1.0.0   # 覆盖版本基线
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
TAG_PREFIX = "agyapptitle/"

# 构建生成的临时或输出文件，不计入工作区污染
GENERATED_FILES = (
    "/bin/agyapptitle.exe",
    "/bin/agyapptitlew.exe",
    "/bin/agyapptitle-dev.exe",
    "/bin/agyapptitlew-dev.exe",
)


def setup_console() -> None:
    """统一控制台流编码，避免 GBK 环境乱码或 Emoji 崩溃。"""
    if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
        try:
            sys.stdout.reconfigure(encoding="utf-8", errors="replace")
        except Exception:
            pass
    if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
        try:
            sys.stderr.reconfigure(encoding="utf-8", errors="replace")
        except Exception:
            pass


def run_git(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args],
        cwd=str(PROJECT_ROOT),
        capture_output=True,
        text=True,
        check=False,
    )


def exact_project_tag() -> str | None:
    """HEAD 上精确命中的项目 tag（形如 agyapptitle/v1.0.0）。"""
    r = run_git("tag", "--points-at", "HEAD")
    if r.returncode != 0:
        return None
    for tag in (t.strip() for t in r.stdout.splitlines()):
        if tag.startswith(f"{TAG_PREFIX}v"):
            return tag
    return None


def latest_project_tag() -> str | None:
    """最近一个项目 tag，用于推导 dev 版本基线。"""
    r = run_git("describe", "--tags", "--abbrev=0", "--match", f"{TAG_PREFIX}v*")
    if r.returncode != 0:
        return None
    tag = r.stdout.strip()
    return tag or None


def worktree_dirty() -> bool:
    """当前子项目工作区是否含未提交改动。"""
    r = run_git("status", "--porcelain", ".")
    if r.returncode != 0:
        return True
    for line in r.stdout.splitlines():
        path = line[3:].strip().strip('"')
        if " -> " in path:
            path = path.split(" -> ", 1)[1]
        normalized = "/" + path.replace("\\", "/").lstrip("/")
        if any(normalized.endswith(suffix) for suffix in GENERATED_FILES):
            continue
        return True
    return False


def get_commit_sha() -> str:
    r = run_git("rev-parse", "--short", "HEAD")
    if r.returncode == 0:
        return r.stdout.strip()
    return "unknown"


def main() -> int:
    setup_console()

    parser = argparse.ArgumentParser(description="构建 agyapptitle 双通道二进制（严格遵循 Monorepo 规范）")
    parser.add_argument("--release", action="store_true", help="声明发布构建；tag 或工作区条件不满足时直接报错")
    parser.add_argument("--version", default="", help="覆盖版本基线")
    args = parser.parse_args()

    BIN_DIR.mkdir(parents=True, exist_ok=True)

    # 1. 严格判定构建类型：仅 精确 tag + 工作区干净 才允许产出 release
    exact_tag = exact_project_tag()
    dirty = worktree_dirty()
    release = exact_tag is not None and not dirty

    if args.release and not release:
        reason = "当前 HEAD 未命中项目 tag" if exact_tag is None else "工作区存在未提交改动"
        print(f"[-] 拒绝发布构建：{reason}（release 需同时满足 精确 tag + 工作区干净）", file=sys.stderr)
        return 2

    # 2. 版本号解析与派生
    if exact_tag:
        base_ver = exact_tag[len(TAG_PREFIX) :].lstrip("v")
    elif args.version:
        base_ver = args.version.lstrip("v")
    else:
        latest = latest_project_tag()
        base_ver = latest[len(TAG_PREFIX) :].lstrip("v") if latest else "1.0.0"

    commit = get_commit_sha()
    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")

    if release:
        mode = "release"
        version = f"v{base_ver}"
        build_id = f"{version}.{commit}"
        cli_name = "agyapptitle.exe"
        daemon_name = "agyapptitlew.exe"
    else:
        mode = "dev"
        dirty_suffix = "-dirty" if dirty else ""
        version = f"v{base_ver}-dev{dirty_suffix}"
        build_id = f"{commit}{dirty_suffix}.{timestamp}"
        cli_name = "agyapptitle-dev.exe"
        daemon_name = "agyapptitlew-dev.exe"

    ldflags = (
        f"-s -w "
        f"-X '{MODULE_PATH}/internal/buildinfo.Version={version}' "
        f"-X '{MODULE_PATH}/internal/buildinfo.BuildID={build_id}' "
        f"-X '{MODULE_PATH}/internal/buildinfo.BuildMode={mode}'"
    )

    print("========================================================")
    print(f"[*] 开始构建 agyapptitle [{mode.upper()} 模式]")
    print(f"[*] Version:   {version}")
    print(f"[*] BuildID:   {build_id}")
    print(f"[*] 命中 Tag:  {exact_tag or '(无)'}")
    print(f"[*] 工作区状态: {'脏 (dirty)' if dirty else '干净 (clean)'}")
    print(f"[*] 产物目录:  {BIN_DIR}")
    print("========================================================\n")

    # 3. 编译控制台 CLI 版
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
        print("[-] CLI 构建失败", file=sys.stderr)
        return res_cli.returncode
    print(f"[+] CLI 构建成功 -> {cli_out}")

    # 4. 编译无黑框后台守护版 (-H=windowsgui)
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
        print("[-] 后台版构建失败", file=sys.stderr)
        return res_daemon.returncode
    print(f"[+] 后台版构建成功 -> {daemon_out}")

    print(f"\n[OK] agyapptitle [{mode.upper()}] 双二进制构建全部完成！\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
