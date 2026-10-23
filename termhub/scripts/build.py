# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""termhub 单通道（GUI）构建：dev / release 严格隔离，统一输出到 bin/。

产物规则：
  - dev（默认）：   bin/termhub-dev.exe（版本号 vX.Y.Z-dev[-dirty]）
  - release：       bin/termhub.exe（正式名，严禁 dev 标识）
只有 HEAD 精确命中 termhub/v* tag 且工作区干净时才允许 release；
dev 与 release 产物名不同，互不覆盖。

附带步骤（可跳过）：
  - 前端打包：frontend/*.ts -> internal/web/static/app.js（esbuild，需 node）
  - PE 资源  ：VERSIONINFO + manifest + icon（go-winres，需 %GOPATH%/bin/go-winres.exe）

用法：
  uv run scripts/build.py              # dev 构建（含前端打包）
  uv run scripts/build.py --release    # 发布构建（条件不满足 exit 2）
  uv run scripts/build.py --version 0.1.0   # 覆盖版本基线，不改变通道判定
  uv run scripts/build.py --skip-frontend   # 跳过 esbuild（前端未改时用）
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
# 产物统一落 bin/；勿用 build/（Wails 资源目录）或 dist/（前端产物）。
OUT_DIR = PROJECT_ROOT / "bin"
TAG_PREFIX = "termhub/"

WINRES_EXE = shutil.which("go-winres") or str(
    Path(os.environ.get("GOPATH", str(Path.home() / "go"))) / "bin" / "go-winres.exe"
)
WINRES_DIR = PROJECT_ROOT / "winres"
MANIFEST = PROJECT_ROOT / "wails.exe.manifest"
ICON = PROJECT_ROOT / "internal" / "app" / "icon.ico"
SYSO_OUT = PROJECT_ROOT / "cmd" / "termhub" / "rsrc_windows_amd64.syso"

EXE_RELEASE = OUT_DIR / "termhub.exe"
EXE_DEV = OUT_DIR / "termhub-dev.exe"
MODULE = "github.com/shihao-hub/termhub"

# 构建自身生成的未跟踪文件：不计入「工作区不干净」。
GENERATED_SUFFIXES = (
    "/bin/termhub.exe",
    "/bin/termhub-dev.exe",
    "/cmd/termhub/rsrc_windows_amd64.syso",
    "/winres/.tmp-termhub-winres.json",
    "/internal/web/static/app.js",
)


def setup_console() -> None:
    """统一控制台流编码，避免中文输出乱码。"""
    if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def run_git(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args], cwd=str(PROJECT_ROOT), capture_output=True, text=True, check=False
    )


def exact_project_tag() -> str | None:
    """HEAD 上精确命中的项目 tag（形如 termhub/v1.0.0）。"""
    r = run_git("tag", "--points-at", "HEAD")
    if r.returncode != 0:
        return None
    for tag in (t.strip() for t in r.stdout.splitlines()):
        if tag.startswith(f"{TAG_PREFIX}v"):
            return tag
    return None


def latest_project_tag() -> str | None:
    r = run_git("describe", "--tags", "--abbrev=0", "--match", f"{TAG_PREFIX}v*")
    if r.returncode != 0:
        return None
    return r.stdout.strip() or None


def base_version() -> str:
    tag = latest_project_tag()
    if tag:
        return tag[len(TAG_PREFIX) :].lstrip("v")
    return "0.0.1"


def worktree_dirty() -> bool:
    """工作区是否含构建产物之外的改动；无法判定时保守按脏处理。"""
    r = run_git(
        "-c", "status.relativePaths=false", "status", "--porcelain", "--untracked-files=normal"
    )
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


def four_part_version(version: str) -> str:
    """把 v0.1.0 / 0.1.0-dev 转成 VERSIONINFO 要求的 4 段数字。"""
    core = version.lstrip("v").split("-")[0]
    parts = (core.split(".") + ["0", "0", "0"])[:4]
    return ".".join(parts)


def build_frontend() -> int:
    """esbuild 打包前端 TS 与静态资产到 internal/web/static。"""
    # Windows 下 npx 实为 npx.cmd，CreateProcess 需要完整路径。
    npx = shutil.which("npx") or shutil.which("npx.cmd")
    if not npx:
        print("❌ 缺少 npx（node），无法打包前端；改用 --skip-frontend 可跳过", file=sys.stderr)
        return 4
    r = subprocess.run(
        [
            npx, "esbuild", "frontend/main.ts",
            "--bundle", "--outfile=internal/web/static/app.js", "--minify",
        ],
        cwd=str(PROJECT_ROOT),
    )
    if r.returncode != 0:
        print(f"❌ esbuild 打包失败（exit {r.returncode}）", file=sys.stderr)
        return r.returncode
    # 静态资产同步（源在 frontend/，链接器经 embed 吃 internal/web/static/）。
    import shutil as _sh
    for name in ("index.html", "style.css"):
        _sh.copy2(PROJECT_ROOT / "frontend" / name, PROJECT_ROOT / "internal/web/static" / name)
    print("✅ 前端已打包 → internal/web/static")
    return 0


def build_resources(release: bool, out_exe: Path, version: str) -> int:
    """生成 PE 资源段（VERSIONINFO + manifest + icon）到主包 syso。"""
    if not Path(WINRES_EXE).exists():
        print(f"❌ 缺少 go-winres: {WINRES_EXE}", file=sys.stderr)
        print("   安装：go install github.com/tc-hib/go-winres@latest", file=sys.stderr)
        return 4
    if not ICON.exists():
        print(f"❌ 缺少图标文件: {ICON}", file=sys.stderr)
        return 4
    template = WINRES_DIR / "gui.json"
    if not template.exists():
        print(f"❌ 缺少资源模板: {template}", file=sys.stderr)
        return 4

    fixed_version = four_part_version(version)
    # go-winres 按「相对 JSON 所在目录」解析资源路径，故临时 JSON 必须放 winres/。
    rel_icon = Path(os.path.relpath(ICON, WINRES_DIR)).as_posix()
    rel_manifest = Path(os.path.relpath(MANIFEST, WINRES_DIR)).as_posix()
    description = "termhub - 终端会话管理器"
    rendered = (
        template.read_text(encoding="utf-8")
        .replace("__ICON__", rel_icon)
        .replace("__MANIFEST__", rel_manifest)
        .replace("__FIXED_VERSION__", fixed_version)
        .replace("__VERSION__", version.lstrip("v"))
        .replace("__DESCRIPTION__", description)
        .replace("__ORIGINAL_NAME__", out_exe.name)
    )

    winres_json = WINRES_DIR / ".tmp-termhub-winres.json"
    try:
        winres_json.write_text(rendered, encoding="utf-8", newline="\n")
        r = subprocess.run(
            [
                str(WINRES_EXE), "make",
                "--in", str(winres_json),
                "--arch", "amd64",
                "--out", str(SYSO_OUT.parent / "rsrc"),
            ],
            cwd=str(PROJECT_ROOT),
        )
    finally:
        winres_json.unlink(missing_ok=True)

    if r.returncode != 0:
        print(f"❌ go-winres 生成 syso 失败（exit {r.returncode}）", file=sys.stderr)
        return r.returncode

    print(f"✅ PE 资源已注入: VERSIONINFO={fixed_version} icon={ICON.name} → {SYSO_OUT.name}")
    return 0


def main() -> int:
    setup_console()

    parser = argparse.ArgumentParser(description="构建 termhub（dev / release 严格隔离）")
    parser.add_argument("--release", action="store_true", help="声明发布构建；tag 或工作区条件不满足时报错")
    parser.add_argument("--version", default="", help="覆盖版本基线（不改变 dev/release 判定）")
    parser.add_argument("--skip-frontend", action="store_true", help="跳过 esbuild 前端打包")
    args = parser.parse_args()

    # 1. 判定构建类型：默认 dev，仅 精确 tag + 干净工作区 才 release
    exact_tag = exact_project_tag()
    dirty = worktree_dirty()
    release = exact_tag is not None and not dirty

    if args.release and not release:
        reason = "当前 HEAD 未命中项目 tag" if exact_tag is None else "工作区存在未提交改动"
        print(f"❌ 拒绝发布构建：{reason}（release 需同时满足 精确 tag + 工作区干净）", file=sys.stderr)
        return 2

    base = args.version.lstrip("v") if args.version else base_version()
    version = f"v{base}" if release else f"v{base}-dev" + ("-dirty" if dirty else "")
    build_mode = "release" if release else "dev"

    print(f"📦 构建配置: Version={version}, Mode={build_mode} (tag={exact_tag or '-'}, dirty={dirty})")

    OUT_DIR.mkdir(parents=True, exist_ok=True)

    # 2. 前端打包
    if not args.skip_frontend:
        rc = build_frontend()
        if rc != 0:
            return rc

    # 3. PE 资源
    out_exe = EXE_RELEASE if release else EXE_DEV
    rc = build_resources(release, out_exe, version)
    if rc != 0:
        return rc

    # 4. go build（GUI 零黑框，-H windowsgui 经资源模板 manifest 定义子系统）
    flags = ["-w", "-s"]
    flags.append(f"-X {MODULE}/internal/version.Version={version}")
    flags.append(f"-X {MODULE}/internal/version.BuildMode={build_mode}")

    go_cmd = [
        "go", "build", "-trimpath",
        f"-ldflags={' '.join(flags)}",
        "-o", str(out_exe), "./cmd/termhub",
    ]
    print(f"🔨 [GUI] {out_exe.name}")
    r = subprocess.run(go_cmd, cwd=str(PROJECT_ROOT))
    if r.returncode != 0:
        print(f"❌ go build 失败（exit {r.returncode}）: {out_exe.name}", file=sys.stderr)
        return r.returncode

    # 5. 产物名守卫：dev 必须带 -dev 后缀，release 不得带
    if release and "dev" in out_exe.stem:
        print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
        return 3
    if not release and not out_exe.stem.endswith("-dev"):
        print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
        return 3

    size_mb = out_exe.stat().st_size / (1024 * 1024)
    print(f"🎉 构建完成 [GUI]: {out_exe} ({size_mb:.2f} MB)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
