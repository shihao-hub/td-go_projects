# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""Anatomy Manager 单文件二进制构建流水线。

产物规则（严格隔离）：
  - dev（默认）：anatomymanager-dev.exe（并同步覆盖当前根目录 anatomymanager.exe 便于直接测试运行），版本 vX.Y.Z-dev（脏工作区再加 -dirty）
  - release：anatomymanager.exe，版本 vX.Y.Z
  只有 HEAD 精确命中 anatomymanager/v* tag 且工作区干净时才允许 release；

构建内容：
  - 自动渲染/更新 Windows 多尺寸专属图标
  - 调用 rsrc 将 PE Manifest 与 Icon 资源编译注入 cmd/anatomymanager/*.syso
  - 编译单二进制：静态链接、去符号、-H windowsgui 零黑框

用法：
  uv run scripts/build.py              # 开发构建 (dev)
  uv run scripts/build.py --release    # 发布构建（条件不满足时直接报错退出）
  uv run scripts/build.py --version 1.2.0   # 仅覆盖版本基线，不改变 dev/release 判定
"""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
TAG_PREFIX = "anatomymanager/"
RSRC_EXE = Path(r"C:\Users\29580\go\bin\rsrc.exe")

VERSION_FILE = PROJECT_ROOT / "info.json"
MANIFEST = PROJECT_ROOT / "wails.exe.manifest"
ICON = PROJECT_ROOT / "icon.ico"
SYSO_OUT = PROJECT_ROOT / "cmd" / "anatomymanager" / "rsrc_windows_amd64.syso"
EXE_RELEASE = PROJECT_ROOT / "anatomymanager.exe"
EXE_DEV = PROJECT_ROOT / "anatomymanager-dev.exe"
RENDER_ICON = PROJECT_ROOT / "scripts" / "render_icon.py"

# 构建自身生成的未跟踪文件：不计入「工作区不干净」
GENERATED_SUFFIXES = (
    "/anatomymanager.exe",
    "/anatomymanager-dev.exe",
    "/cmd/anatomymanager/rsrc_windows_amd64.syso",
    "/icon.ico",
    "/icon.png",
)


def setup_console() -> None:
    """统一控制台流编码，避免中文输出乱码。"""
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
    """HEAD 上精确命中的项目 tag（形如 anatomymanager/v1.0.0）。"""
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


def base_version() -> str:
    tag = latest_project_tag()
    if tag:
        return tag[len(TAG_PREFIX) :].lstrip("v")
    return str(json.loads(VERSION_FILE.read_text(encoding="utf-8"))["fixed"]["file_version"]).strip()


def worktree_dirty() -> bool:
    """工作区是否含构建产物之外的改动；无法判定时保守按脏处理。"""
    r = run_git("-c", "status.relativePaths=false", "status", "--porcelain", "--untracked-files=normal")
    if r.returncode != 0:
        return True
    for line in r.stdout.splitlines():
        path = line[3:].strip().strip('"')
        if " -> " in path:
            path = path.split(" -> ", 1)[1]
        normalized = "/" + path.replace("\\", "/").lstrip("/")
        if any(normalized.endswith(suffix) for suffix in GENERATED_SUFFIXES):
            continue
        return True
    return False


def build_resources() -> int:
    """重绘图标并编译 PE 资源段。"""
    if RENDER_ICON.exists():
        r = subprocess.run(["uv", "run", str(RENDER_ICON)], cwd=str(PROJECT_ROOT))
        if r.returncode != 0:
            print("⚠️ 图标渲染脚本执行异常，跳过重绘", file=sys.stderr)

    r = subprocess.run(
        [
            str(RSRC_EXE),
            "-manifest", str(MANIFEST),
            "-ico", str(ICON),
            "-arch", "amd64",
            "-o", str(SYSO_OUT),
        ],
        cwd=str(PROJECT_ROOT),
    )
    if r.returncode != 0:
        print(f"❌ rsrc 编译 syso 失败（exit {r.returncode}）", file=sys.stderr)
        return r.returncode
    print(f"✅ 已注入 Windows PE 资源段: {SYSO_OUT}")
    return 0


def main() -> int:
    setup_console()

    parser = argparse.ArgumentParser(description="构建 anatomymanager 单二进制（dev / release 严格隔离）")
    parser.add_argument("--release", action="store_true", help="声明发布构建；tag 或工作区条件不满足时直接报错")
    parser.add_argument("--version", default="", help="覆盖版本基线（不改变 dev/release 判定）")
    args = parser.parse_args()

    # 1. 图标与 PE 资源
    rc = build_resources()
    if rc != 0:
        return rc

    # 2. 判定构建类型：默认 dev，仅 精确 tag + 干净工作区 才 release
    exact_tag = exact_project_tag()
    dirty = worktree_dirty()
    release = exact_tag is not None and not dirty

    if args.release and not release:
        reason = "当前 HEAD 未命中项目 tag" if exact_tag is None else "工作区存在未提交改动"
        print(f"❌ 拒绝发布构建：{reason}（release 需同时满足 精确 tag + 工作区干净）", file=sys.stderr)
        return 2

    base = args.version.lstrip("v") if args.version else base_version()
    if release:
        version = f"v{base}"
    else:
        version = f"v{base}-dev" + ("-dirty" if dirty else "")
    build_mode = "release" if release else "dev"

    print(
        f"📦 构建配置: Version={version}, Mode={build_mode} "
        f"(tag={exact_tag or '-'}, dirty={dirty})"
    )

    # 3. 执行 go build
    ldflags = (
        f"-w -s -H windowsgui "
        f"-X anatomymanager/internal/app.Version={version} "
        f"-X anatomymanager/internal/app.BuildMode={build_mode}"
    )
    out_exe = EXE_RELEASE if release else EXE_DEV
    r = subprocess.run(
        [
            "go", "build", "-trimpath",
            f"-ldflags={ldflags}",
            "-o", str(out_exe),
            "./cmd/anatomymanager",
        ],
        cwd=str(PROJECT_ROOT),
    )
    if r.returncode != 0:
        print(f"❌ go build 失败（exit {r.returncode}）", file=sys.stderr)
        return r.returncode

    # 4. 产物名守卫与同步
    if release and "dev" in out_exe.stem:
        print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
        return 3
    if not release and not out_exe.stem.endswith("-dev"):
        print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
        return 3

    # 开发模式同步至 anatomymanager.exe，确保用户运行 ./anatomymanager.exe 立即生效
    if not release:
        try:
            shutil.copy2(out_exe, EXE_RELEASE)
            print(f"🔄 已同步更新当前根目录运行产物: {EXE_RELEASE}")
        except Exception as e:
            print(f"⚠️ 无法同步至 {EXE_RELEASE}（可能被运行中进程锁定）: {e}", file=sys.stderr)

    size_mb = out_exe.stat().st_size / (1024 * 1024)
    print(f"🎉 构建完成: {out_exe} ({size_mb:.2f} MB)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
