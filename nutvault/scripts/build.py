# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""NutVault 单文件二进制构建流水线。

产物规则（严格隔离，每条通道出两个二进制，命名固定为 `nutvault-<通道>[-dev].exe`）：
  - dev（默认）：nutvault-gui-dev.exe（GUI 通道）+ nutvault-cli-dev.exe（Console 通道）
  - release：nutvault-gui.exe（GUI 通道）+ nutvault-cli.exe（Console 通道）
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = PROJECT_ROOT / "bin"
TAG_PREFIX = "nutvault/"

WINRES_EXE = Path(os.environ.get("GO_WINRES") or r"C:\Users\29580\go\bin\go-winres.exe")
WINRES_DIR = PROJECT_ROOT / "winres"

VERSION_FILE = PROJECT_ROOT / "info.json"
MANIFEST = PROJECT_ROOT / "wails.exe.manifest"
ICON_RELEASE = PROJECT_ROOT / "icon.ico"
ICON_DEV = PROJECT_ROOT / "internal" / "app" / "icon_dev.ico"
SYSO_OUT = PROJECT_ROOT / "cmd" / "nutvault" / "rsrc_windows_amd64.syso"
EXE_GUI_RELEASE = OUT_DIR / "nutvault-gui.exe"
EXE_GUI_DEV = OUT_DIR / "nutvault-gui-dev.exe"
EXE_CLI_RELEASE = OUT_DIR / "nutvault-cli.exe"
EXE_CLI_DEV = OUT_DIR / "nutvault-cli-dev.exe"
RENDER_ICON = PROJECT_ROOT / "scripts" / "render_icon.py"


def setup_console() -> None:
    if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def run(cmd: list[str], cwd: Path | None = None, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        cmd,
        cwd=cwd or PROJECT_ROOT,
        check=check,
        text=True,
        capture_output=True,
        encoding="utf-8",
        errors="replace",
    )


def read_base_version() -> str:
    if not VERSION_FILE.exists():
        return "1.0.0"
    try:
        data = json.loads(VERSION_FILE.read_text(encoding="utf-8"))
        return str(data.get("fixed", {}).get("file_version") or "1.0.0")
    except Exception:
        return "1.0.0"


def git_commit() -> str:
    try:
        cp = run(["git", "rev-parse", "--short=8", "HEAD"], check=False)
        return cp.stdout.strip() if cp.returncode == 0 else "unknown"
    except Exception:
        return "unknown"


def determine_mode(force_release: bool) -> tuple[bool, str]:
    base = read_base_version()
    if force_release:
        return True, base
    commit = git_commit()
    return False, f"dev-{commit}"


def ensure_icon(is_release: bool) -> Path:
    if not is_release:
        if not ICON_DEV.exists():
            raise RuntimeError(f"开发通道图标缺失: {ICON_DEV}")
        return ICON_DEV

    if not ICON_RELEASE.exists():
        print("未检测到 release 图标，正在渲染...")
        run([sys.executable, str(RENDER_ICON)])
    return ICON_RELEASE


def compile_winres(target: str, version_str: str, icon_path: Path, is_release: bool) -> None:
    if not WINRES_EXE.exists():
        raise RuntimeError(f"go-winres 不存在: {WINRES_EXE}")

    template_file = WINRES_DIR / f"{target}.json"
    content = template_file.read_text(encoding="utf-8")

    nums = [s for s in version_str.replace("v", "").replace("-", ".").split(".") if s.isdigit()]
    while len(nums) < 4:
        nums.append("0")
    fixed_ver = ".".join(nums[:4])

    suffix = "" if is_release else "-dev"
    orig_name = f"nutvault-{target}{suffix}.exe"

    rel_icon = Path(os.path.relpath(icon_path, WINRES_DIR)).as_posix()
    rel_manifest = Path(os.path.relpath(MANIFEST, WINRES_DIR)).as_posix()

    content = (
        content.replace("__ICON__", rel_icon)
        .replace("__MANIFEST__", rel_manifest)
        .replace("__VERSION__", version_str.lstrip("v"))
        .replace("__FIXED_VERSION__", fixed_ver)
        .replace("__ORIGINAL_NAME__", orig_name)
    )

    tmp_json = WINRES_DIR / ".tmp-winres.json"
    tmp_json.write_text(content, encoding="utf-8")

    try:
        cmd = [
            str(WINRES_EXE),
            "make",
            "--in",
            str(tmp_json),
            "--out",
            str(SYSO_OUT.parent / "rsrc"),
            "--arch",
            "amd64",
        ]
        res = run(cmd)
        if res.returncode != 0:
            raise RuntimeError(f"go-winres 失败: {res.stderr}")
    finally:
        if tmp_json.exists():
            tmp_json.unlink()


def build_channel(is_release: bool, version: str) -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    mode_str = "release" if is_release else "dev"
    icon = ensure_icon(is_release)

    print(f"\n=== 开始构建 NutVault [{mode_str.upper()}] 通道 (版本: {version}) ===")

    # 1. 构建 GUI 通道
    print("[1/2] 正在编译 GUI 二进制 (nutvault-gui)...")
    compile_winres("gui", version, icon, is_release)
    gui_out = EXE_GUI_RELEASE if is_release else EXE_GUI_DEV
    gui_tags = [] if is_release else ["dev"]
    ldflags = [
        "-s", "-w",
        "-H", "windowsgui",
        f"-X nutvault/internal/app.Version={version}",
        f"-X nutvault/internal/app.BuildMode={mode_str}",
    ]
    cmd_gui = ["go", "build", "-trimpath", "-ldflags", " ".join(ldflags)]
    if gui_tags:
        cmd_gui.extend(["-tags", ",".join(gui_tags)])
    cmd_gui.extend(["-o", str(gui_out), "./cmd/nutvault"])

    run(cmd_gui)
    print(f"  ✓ 产物输出: {gui_out}")

    # 2. 构建 CLI 通道
    print("[2/2] 正在编译 CLI 二进制 (nutvault-cli)...")
    compile_winres("cli", version, icon, is_release)
    cli_out = EXE_CLI_RELEASE if is_release else EXE_CLI_DEV
    cli_tags = ["cli"]
    if not is_release:
        cli_tags.append("dev")
    ldflags_cli = [
        "-s", "-w",
        f"-X nutvault/internal/app.Version={version}",
        f"-X nutvault/internal/app.BuildMode={mode_str}",
    ]
    cmd_cli = [
        "go", "build", "-trimpath",
        "-tags", ",".join(cli_tags),
        "-ldflags", " ".join(ldflags_cli),
        "-o", str(cli_out),
        "./cmd/nutvault",
    ]
    run(cmd_cli)
    print(f"  ✓ 产物输出: {cli_out}")

    # 清理 syso
    if SYSO_OUT.exists():
        SYSO_OUT.unlink()

    print("\n✓ 双通道构建完成！")


def main() -> None:
    setup_console()
    parser = argparse.ArgumentParser(description="NutVault 构建脚本")
    parser.add_argument("--release", action="store_true", help="构建 release 发行版")
    args = parser.parse_args()

    is_release, version = determine_mode(args.release)
    build_channel(is_release, version)


if __name__ == "__main__":
    main()
