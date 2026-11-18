# /// script
# requires-python = ">=3.11"
# ///
"""ccquota 构建流水线。

产物规则：
  - dev（默认）：bin/ccquota-dev.exe
  - release：bin/ccquota.exe（仅当 HEAD 精确命中 ccquota/v* tag 且工作区干净）

用法：
  uv run scripts/build.py
  uv run scripts/build.py --release
  uv run scripts/build.py --version 0.2.0
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
OUT_DIR = PROJECT_ROOT / "bin"
TAG_PREFIX = "ccquota/"

WINRES_EXE = Path(os.environ.get("GO_WINRES") or r"C:\Users\29580\go\bin\go-winres.exe")
WINRES_DIR = PROJECT_ROOT / "winres"
SYSO_DIR = PROJECT_ROOT / "cmd" / "ccquota"
VERSION_FILE = PROJECT_ROOT / "info.json"

# 默认图标来源：父仓 docs/assets；子仓独立使用时可能不存在，届时跳过图标注入。
DEFAULT_ICON = PROJECT_ROOT.parent.parent / "docs" / "assets" / "projects" / "go_projects" / "go-default.ico"
ICON_LOCAL = PROJECT_ROOT / "icon.ico"


def setup_console() -> None:
    for stream in (sys.stdout, sys.stderr):
        enc = (stream.encoding or "").lower()
        if enc not in ("utf-8", "utf8"):
            stream.reconfigure(encoding="utf-8", errors="replace")


def run_git(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(["git", *args], cwd=str(PROJECT_ROOT), capture_output=True, text=True, check=False)


def exact_project_tag() -> str | None:
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
        return tag[len(TAG_PREFIX):].lstrip("v")
    return str(json.loads(VERSION_FILE.read_text(encoding="utf-8"))["fixed"]["file_version"]).strip()


def worktree_dirty() -> bool:
    r = run_git("-c", "status.relativePaths=false", "status", "--porcelain", "--untracked-files=normal")
    if r.returncode != 0:
        return True
    ignored = (
        "/bin/ccquota",
        "/cmd/ccquota/rsrc_windows_amd64.syso",
        "/cmd/ccquota-gui/rsrc_windows_amd64.syso",
        "/icon.ico",
        "/winres/.tmp-",
    )
    for line in r.stdout.splitlines():
        path = "/" + line[3:].strip().strip('"').replace("\\", "/").lstrip("/")
        if any(path.endswith(s) or path.startswith(s) for s in ignored):
            continue
        return True
    return False


def four_part(version: str) -> str:
    core = version.lstrip("v").split("-")[0]
    return ".".join((core.split(".") + ["0", "0", "0"])[:4])


def ensure_icon() -> Path | None:
    if DEFAULT_ICON.exists():
        shutil.copyfile(DEFAULT_ICON, ICON_LOCAL)
        return ICON_LOCAL
    if ICON_LOCAL.exists():
        return ICON_LOCAL
    return None


def build_resources(version: str, out_name: str, gui: bool, syso_dir: Path) -> int:
    icon = ensure_icon()
    if icon is None:
        print("⚠️ 未找到图标，跳过 PE 资源注入", file=sys.stderr)
        return 0
    if not WINRES_EXE.exists():
        print(f"⚠️ 缺少 go-winres（{WINRES_EXE}），跳过 PE 资源注入", file=sys.stderr)
        return 0

    template = WINRES_DIR / ("gui.json" if gui else "cli.json")
    rendered = (
        template.read_text(encoding="utf-8")
        .replace("__ICON__", Path(os.path.relpath(icon, WINRES_DIR)).as_posix())
        .replace("__FIXED_VERSION__", four_part(version))
        .replace("__VERSION__", version.lstrip("v"))
        .replace("__ORIGINAL_NAME__", out_name)
    )
    tmp = WINRES_DIR / ".tmp-winres.json"
    try:
        tmp.write_text(rendered, encoding="utf-8", newline="\n")
        r = subprocess.run(
            [str(WINRES_EXE), "make", "--in", str(tmp), "--arch", "amd64", "--out", str(syso_dir / "rsrc")],
            cwd=str(PROJECT_ROOT),
        )
    finally:
        tmp.unlink(missing_ok=True)
    return r.returncode


def main() -> int:
    setup_console()
    parser = argparse.ArgumentParser(description="构建 ccquota（dev / release 严格隔离）")
    parser.add_argument("--release", action="store_true", help="声明发布构建；条件不满足时退出码 2")
    parser.add_argument("--version", default="", help="覆盖版本基线")
    args = parser.parse_args()

    tag = exact_project_tag()
    dirty = worktree_dirty()
    release = tag is not None and not dirty
    if args.release and not release:
        reason = "HEAD 未命中项目 tag" if tag is None else "工作区存在未提交改动"
        print(f"❌ 拒绝发布构建：{reason}", file=sys.stderr)
        return 2

    base = args.version.lstrip("v") if args.version else base_version()
    version = f"v{base}" if release else f"v{base}-dev" + ("-dirty" if dirty else "")
    mode = "release" if release else "dev"
    print(f"📦 Version={version}, Mode={mode} (tag={tag or '-'}, dirty={dirty})")

    out_cli = OUT_DIR / (f"ccquota{'' if release else '-dev'}.exe")
    out_gui = OUT_DIR / (f"ccquota-gui{'' if release else '-dev'}.exe")
    OUT_DIR.mkdir(parents=True, exist_ok=True)

    ldflags = " ".join([
        "-w", "-s",
        f"-X ccquota/internal/version.Version={version}",
        f"-X ccquota/internal/version.BuildMode={mode}",
    ])

    targets = [
        (out_cli, False, []),
        (out_gui, True, ["-tags", "desktop,production", "-ldflags", ldflags + " -H windowsgui"]),
    ]
    for out_exe, gui, extra in targets:
        syso_dir = PROJECT_ROOT / "cmd" / ("ccquota-gui" if gui else "ccquota")
        rc = build_resources(version, out_exe.name, gui, syso_dir)
        if rc != 0:
            print("❌ PE 资源注入失败", file=sys.stderr)
            return rc

        cmd = ["go", "build", "-trimpath"]
        if extra:
            cmd += extra
        else:
            cmd += [f"-ldflags={ldflags}"]
        cmd += ["-o", str(out_exe), "./cmd/ccquota-gui" if gui else "./cmd/ccquota"]
        print(f"🔨 [{'GUI' if gui else 'CLI'}] {out_exe.name}")
        r = subprocess.run(cmd, cwd=str(PROJECT_ROOT))
        if r.returncode != 0:
            print(f"❌ go build 失败: {out_exe.name}", file=sys.stderr)
            return r.returncode

    for out_exe, _gui, _ in targets:
        if release and "dev" in out_exe.stem:
            print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
            return 3
        if not release and not out_exe.stem.endswith("-dev"):
            print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
            return 3

    for out_exe, gui, _ in targets:
        size = out_exe.stat().st_size / (1024 * 1024)
        print(f"🎉 [{'GUI' if gui else 'CLI'}] {out_exe.name} ({size:.2f} MB)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
