# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""rebootremind 构建脚本（dev / release 严格隔离）。

产物规则：
  - dev（默认）：bin/rebootremind-dev.exe
  - release：bin/rebootremind.exe
仅当 HEAD 精确命中 rebootremind/v* tag 且工作区干净时才允许 release；
dev 与 release 产物名不同，互不覆盖。

构建前会用 rsrc 把图标与 manifest（启用 Common Controls v6）写入
cmd/rebootremind/rsrc_windows_amd64.syso。

用法：
  uv run scripts/build.py              # dev 构建
  uv run scripts/build.py --release    # 发布构建（条件不满足 exit 2）
  uv run scripts/build.py --version 0.2.0   # 覆盖版本基线，不改变通道判定
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = PROJECT_ROOT / "bin"
TAG_PREFIX = "rebootremind/"
MAIN_PKG = PROJECT_ROOT / "cmd" / "rebootremind"
ICON = MAIN_PKG / "icon.ico"
MANIFEST = MAIN_PKG / "rebootremind.exe.manifest"
SYSO = MAIN_PKG / "rsrc_windows_amd64.syso"
RSRC_EXE = shutil.which("rsrc") or str(
    Path(os.environ.get("GOPATH", str(Path.home() / "go"))) / "bin" / "rsrc.exe"
)

GENERATED_SUFFIXES = (
    "/bin/rebootremind.exe",
    "/bin/rebootremind-dev.exe",
    "/cmd/rebootremind/rsrc_windows_amd64.syso",
)


def setup_console() -> None:
    if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def run_git(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args],
        cwd=str(PROJECT_ROOT),
        capture_output=True,
        text=True,
        check=False,
    )


def exact_project_tag() -> str | None:
    result = run_git("tag", "--points-at", "HEAD")
    if result.returncode != 0:
        return None
    for tag in (line.strip() for line in result.stdout.splitlines()):
        if tag.startswith(f"{TAG_PREFIX}v"):
            return tag
    return None


def latest_project_tag() -> str | None:
    result = run_git("describe", "--tags", "--abbrev=0", "--match", f"{TAG_PREFIX}v*")
    if result.returncode != 0:
        return None
    tag = result.stdout.strip()
    return tag or None


def worktree_dirty() -> bool:
    result = run_git("-c", "status.relativePaths=false", "status", "--porcelain", "--untracked-files=normal")
    if result.returncode != 0:
        return True
    for line in result.stdout.splitlines():
        path = line[3:].strip().strip('"')
        if " -> " in path:
            path = path.split(" -> ", 1)[1]
        normalized = "/" + path.replace("\\", "/").lstrip("/")
        if any(normalized.endswith(suffix) for suffix in GENERATED_SUFFIXES):
            continue
        return True
    return False


def base_version() -> str:
    tag = latest_project_tag()
    if tag:
        return tag[len(TAG_PREFIX) :].lstrip("v")
    return "0.1.0"


def build_resources() -> int:
    if not Path(RSRC_EXE).exists():
        print(f"❌ 缺少 rsrc: {RSRC_EXE}", file=sys.stderr)
        print("   安装：go install github.com/akavel/rsrc@latest", file=sys.stderr)
        return 4
    if not ICON.exists():
        print(f"❌ 缺少图标: {ICON}", file=sys.stderr)
        print("   从 docs/assets/projects/go_projects/go-default.ico 复制到主包目录", file=sys.stderr)
        return 4
    if not MANIFEST.exists():
        print(f"❌ 缺少 manifest: {MANIFEST}", file=sys.stderr)
        return 4

    result = subprocess.run(
        [
            str(RSRC_EXE),
            "-ico",
            str(ICON),
            "-manifest",
            str(MANIFEST),
            "-o",
            str(SYSO),
        ],
        cwd=str(PROJECT_ROOT),
    )
    if result.returncode != 0:
        print(f"❌ rsrc 生成 syso 失败（exit {result.returncode}）", file=sys.stderr)
        return result.returncode
    print(f"✅ PE 资源已注入: icon={ICON.name} manifest={MANIFEST.name}")
    return 0


def main() -> int:
    setup_console()

    parser = argparse.ArgumentParser(description="构建 rebootremind（dev / release 隔离）")
    parser.add_argument("--release", action="store_true", help="声明发布构建；tag 或工作区条件不满足时报错")
    parser.add_argument("--dev", action="store_true", help="显式声明 dev 构建（默认行为，保留以便脚本调用）")
    parser.add_argument("--version", default="", help="覆盖版本基线（不改变 dev/release 判定）")
    args = parser.parse_args()

    if args.release and args.dev:
        print("❌ --release 与 --dev 不能同时使用", file=sys.stderr)
        return 2

    tag = exact_project_tag()
    dirty = worktree_dirty()
    release = tag is not None and not dirty

    if args.release and not release:
        reason = "当前 HEAD 未命中 rebootremind/v* tag" if tag is None else "工作区存在未提交改动"
        print(f"❌ 拒绝发布构建：{reason}（release 需同时满足 精确 tag + 工作区干净）", file=sys.stderr)
        return 2

    base = args.version.lstrip("v") if args.version else base_version()
    if release:
        version = f"v{base}"
    else:
        version = f"v{base}-dev" + ("-dirty" if dirty else "")
    mode = "release" if release else "dev"
    rev = run_git("rev-parse", "--short", "HEAD").stdout.strip() or "unknown"
    build_id = rev + ("-dirty" if dirty else "")

    print(f"📦 构建配置: Version={version}, Mode={mode} (tag={tag or '-'}, dirty={dirty})")

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    rc = build_resources()
    if rc != 0:
        return rc

    out_exe = OUT_DIR / ("rebootremind.exe" if release else "rebootremind-dev.exe")
    ldflags = " ".join(
        [
            "-s",
            "-w",
            f"-X rebootremind/internal/buildinfo.Version={version}",
            f"-X rebootremind/internal/buildinfo.Mode={mode}",
            f"-X rebootremind/internal/buildinfo.BuildID={build_id}",
        ]
    )
    go_cmd = ["go", "build", "-trimpath", f"-ldflags={ldflags}", "-o", str(out_exe), "./cmd/rebootremind"]

    print(f"🔨 {out_exe.name}")
    result = subprocess.run(go_cmd, cwd=str(PROJECT_ROOT))
    if result.returncode != 0:
        print(f"❌ go build 失败（exit {result.returncode}）", file=sys.stderr)
        return result.returncode

    if release and out_exe.stem.endswith("-dev"):
        print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
        return 3
    if not release and not out_exe.stem.endswith("-dev"):
        print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
        return 3

    size_mb = out_exe.stat().st_size / (1024 * 1024)
    print(f"🎉 构建完成: {out_exe} ({size_mb:.2f} MB)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
