# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""DeepSeek Search Studio 双通道二进制构建流水线。

产物规则（严格隔离，每条通道出两个二进制，命名固定为 `deepseeksearch-<通道>[-dev].exe`）：
  - dev（默认）：deepseeksearch-gui-dev.exe（GUI 通道）+ deepseeksearch-cli-dev.exe（Console 通道）
  - release：deepseeksearch-gui.exe（GUI 通道）+ deepseeksearch-cli.exe（Console 通道）
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
TAG_PREFIX = "deepseeksearch/"

# 查找 go-winres
WINRES_EXE = Path(os.environ.get("GO_WINRES") or r"C:\Users\29580\go\bin\go-winres.exe")
if not WINRES_EXE.exists():
    which_winres = shutil.which("go-winres")
    if which_winres:
        WINRES_EXE = Path(which_winres)

WINRES_DIR = PROJECT_ROOT / "winres"
VERSION_FILE = PROJECT_ROOT / "info.json"
MANIFEST = PROJECT_ROOT / "wails.exe.manifest"
ICON_RELEASE = PROJECT_ROOT / "icon.ico"
ICON_DEV = PROJECT_ROOT / "internal" / "app" / "icon_dev.ico"
SYSO_OUT = PROJECT_ROOT / "cmd" / "deepseeksearch" / "rsrc_windows_amd64.syso"

EXE_GUI_RELEASE = OUT_DIR / "deepseeksearch-gui.exe"
EXE_GUI_DEV = OUT_DIR / "deepseeksearch-gui-dev.exe"
EXE_CLI_RELEASE = OUT_DIR / "deepseeksearch-cli.exe"
EXE_CLI_DEV = OUT_DIR / "deepseeksearch-cli-dev.exe"

GENERATED_SUFFIXES = (
    "/bin/deepseeksearch-gui.exe",
    "/bin/deepseeksearch-gui-dev.exe",
    "/bin/deepseeksearch-cli.exe",
    "/bin/deepseeksearch-cli-dev.exe",
    "/cmd/deepseeksearch/rsrc_windows_amd64.syso",
    "/winres/.tmp-winres.json",
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
    tag = r.stdout.strip()
    return tag or None


def base_version() -> str:
    tag = latest_project_tag()
    if tag:
        return tag[len(TAG_PREFIX) :].lstrip("v")
    if VERSION_FILE.exists():
        return str(json.loads(VERSION_FILE.read_text(encoding="utf-8"))["fixed"]["file_version"]).strip()
    return "1.0.0"


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
        if any(normalized.endswith(suffix) for suffix in GENERATED_SUFFIXES):
            continue
        return True
    return False


def four_part_version(version: str) -> str:
    core = version.lstrip("v").split("-")[0]
    parts = (core.split(".") + ["0", "0", "0"])[:4]
    return ".".join(parts)


def build_resources(release: bool, gui: bool, out_exe: Path, version: str) -> int:
    if not WINRES_EXE.exists():
        print(f"❌ 缺少 go-winres: {WINRES_EXE}", file=sys.stderr)
        return 4

    icon = ICON_RELEASE if release else ICON_DEV
    if not icon.exists():
        print(f"❌ 缺少图标文件: {icon}", file=sys.stderr)
        return 4

    template = WINRES_DIR / ("gui.json" if gui else "cli.json")
    if not template.exists():
        print(f"❌ 缺少资源模板: {template}", file=sys.stderr)
        return 4

    fixed_version = four_part_version(version)
    rel_icon = Path(os.path.relpath(icon, WINRES_DIR)).as_posix()
    rel_manifest = Path(os.path.relpath(MANIFEST, WINRES_DIR)).as_posix()
    rendered = (
        template.read_text(encoding="utf-8")
        .replace("__ICON__", rel_icon)
        .replace("__MANIFEST__", rel_manifest)
        .replace("__FIXED_VERSION__", fixed_version)
        .replace("__VERSION__", version.lstrip("v"))
        .replace("__ORIGINAL_NAME__", out_exe.name)
    )

    winres_json = WINRES_DIR / ".tmp-winres.json"
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

    kind = "GUI" if gui else "CLI"
    print(f"✅ [{kind}] PE 资源已注入: VERSIONINFO={fixed_version} icon={icon.name} → {SYSO_OUT.name}")
    return 0


def sanitize_exe(exe: Path) -> None:
    if not exe.exists() or os.name != "nt":
        return
    # 提权 Medium 避免低完整性标签被继承
    subprocess.run(["icacls", str(exe), "/setintegritylevel", "Medium"], capture_output=True, check=False)


def main() -> int:
    setup_console()

    parser = argparse.ArgumentParser(description="构建 deepseeksearch 双通道二进制（dev / release 严格隔离）")
    parser.add_argument("--release", action="store_true", help="声明发布构建；tag 或工作区条件不满足时直接报错")
    parser.add_argument("--version", default="", help="覆盖版本基线（不改变 dev/release 判定）")
    args = parser.parse_args()

    exact_tag = exact_project_tag()
    dirty = worktree_dirty()
    release = exact_tag is not None and not dirty

    if args.release and not release:
        reason = "当前 HEAD 未命中项目 tag" if exact_tag is None else "工作区存在未提交改动"
        print(f"❌ 拒绝发布构建：{reason}（release 需同时满足 精确 tag + 工作区干净）", file=sys.stderr)
        return 2

    base = args.version.lstrip("v") if args.version else base_version()
    if release:
        version = base
        exe_gui = EXE_GUI_RELEASE
        exe_cli = EXE_CLI_RELEASE
        mode = "release"
    else:
        version = f"{base}-dev"
        if dirty:
            version += "-dirty"
        exe_gui = EXE_GUI_DEV
        exe_cli = EXE_CLI_DEV
        mode = "dev"

    print("=" * 64)
    print(f"DeepSeek Search Studio 构建流水线：通道={mode.upper()} 版本={version}")
    print(f"  GUI 产物：{exe_gui}")
    print(f"  CLI 产物：{exe_cli}")
    print("=" * 64)

    OUT_DIR.mkdir(parents=True, exist_ok=True)

    # 1. 构建 GUI 通道产物
    if rc := build_resources(release, gui=True, out_exe=exe_gui, version=version):
        return rc

    ldflags_gui = [
        "-w", "-s",
        "-H", "windowsgui",
        f"-X deepseeksearch/internal/app.Version={version}",
        f"-X deepseeksearch/internal/app.BuildMode={mode}",
    ]
    if release:
        tags_gui = ["production"]
    else:
        tags_gui = ["dev"]

    cmd_gui = [
        "go", "build",
        "-tags", ",".join(tags_gui),
        "-ldflags", " ".join(ldflags_gui),
        "-o", str(exe_gui),
        "./cmd/deepseeksearch",
    ]
    print(f"\n[1/2] 正在构建 GUI 通道：{exe_gui.name} ...")
    r_gui = subprocess.run(cmd_gui, cwd=str(PROJECT_ROOT))
    if r_gui.returncode != 0:
        print(f"❌ GUI 构建失败 (exit {r_gui.returncode})", file=sys.stderr)
        return r_gui.returncode
    sanitize_exe(exe_gui)
    print(f"✅ GUI 构建成功: {exe_gui}")

    # 2. 构建 CLI 通道产物
    if rc := build_resources(release, gui=False, out_exe=exe_cli, version=version):
        return rc

    ldflags_cli = [
        "-w", "-s",
        f"-X deepseeksearch/internal/app.Version={version}",
        f"-X deepseeksearch/internal/app.BuildMode={mode}",
    ]
    tags_cli = ["cli"]
    if release:
        tags_cli.append("production")
    else:
        tags_cli.append("dev")

    cmd_cli = [
        "go", "build",
        "-tags", ",".join(tags_cli),
        "-ldflags", " ".join(ldflags_cli),
        "-o", str(exe_cli),
        "./cmd/deepseeksearch",
    ]
    print(f"\n[2/2] 正在构建 CLI 通道：{exe_cli.name} ...")
    r_cli = subprocess.run(cmd_cli, cwd=str(PROJECT_ROOT))
    if r_cli.returncode != 0:
        print(f"❌ CLI 构建失败 (exit {r_cli.returncode})", file=sys.stderr)
        return r_cli.returncode
    sanitize_exe(exe_cli)
    print(f"✅ CLI 构建成功: {exe_cli}")

    print("\n" + "=" * 64)
    print(f"🎉 双通道构建全部完成！通道: {mode.upper()}")
    print(f"  • GUI: {exe_gui.name} (大小: {exe_gui.stat().st_size // 1024} KB)")
    print(f"  • CLI: {exe_cli.name} (大小: {exe_cli.stat().st_size // 1024} KB)")
    print("=" * 64)
    return 0


if __name__ == "__main__":
    sys.exit(main())
