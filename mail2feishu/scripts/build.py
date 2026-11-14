# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""mail2feishu 双通道二进制构建流水线。

产物规则（每条通道两个二进制）：
  - dev（默认）：mail2feishu-gui-dev.exe（GUI）+ mail2feishu-cli-dev.exe（Console）
  - release：mail2feishu-gui.exe（GUI）+ mail2feishu-cli.exe（Console）
只有 HEAD 精确命中 mail2feishu/v* tag 且本项目路径工作区干净时才允许 release。

双通道设计：
  - GUI 通道 -H windowsgui，双击零黑框；启动完整服务与托盘；
  - Console 通道为 console 子系统，保留 -check / -test-webhook / -version
    与前台常驻能力，供终端、脚本与 Agent 使用。

图标：release 由 scripts/render_icon.py 生成绿色信封图标，dev 用 Go 地鼠。

用法：
  uv run scripts/build.py              # 开发构建
  uv run scripts/build.py --release    # 发布构建（tag/脏工作区不满足时退出码 2）
  uv run scripts/build.py --version 1.2.0   # 覆盖版本基线
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = PROJECT_ROOT / "bin"
TAG_PREFIX = "mail2feishu/"
MAIN_PKG = "./cmd/mail2feishu"

WINRES_EXE = Path(os.environ.get("GO_WINRES") or r"C:\Users\29580\go\bin\go-winres.exe")
WINRES_DIR = PROJECT_ROOT / "winres"
MANIFEST = PROJECT_ROOT / "wails.exe.manifest"
ICON_RELEASE = PROJECT_ROOT / "icon.ico"
ICON_DEV = PROJECT_ROOT / "internal" / "app" / "icon_dev.ico"
SYSO_OUT = PROJECT_ROOT / "cmd" / "mail2feishu" / "rsrc_windows_amd64.syso"

EXE_GUI_RELEASE = OUT_DIR / "mail2feishu-gui.exe"
EXE_GUI_DEV = OUT_DIR / "mail2feishu-gui-dev.exe"
EXE_CLI_RELEASE = OUT_DIR / "mail2feishu-cli.exe"
EXE_CLI_DEV = OUT_DIR / "mail2feishu-cli-dev.exe"

LEGACY_EXES = (
    PROJECT_ROOT / "mail2feishu.exe",
    PROJECT_ROOT / "mail2feishu-dev.exe",
)

RENDER_ICON = PROJECT_ROOT / "scripts" / "render_icon.py"
DEFAULT_VERSION = "0.2.0"

GENERATED_SUFFIXES = (
    "/bin/mail2feishu-gui.exe",
    "/bin/mail2feishu-gui-dev.exe",
    "/bin/mail2feishu-cli.exe",
    "/bin/mail2feishu-cli-dev.exe",
    "/cmd/mail2feishu/rsrc_windows_amd64.syso",
    "/winres/.tmp-winres.json",
    "/icon.ico",
    "/icon.png",
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


def base_version() -> str:
    tag = latest_project_tag()
    if tag:
        return tag[len(TAG_PREFIX) :].lstrip("v")
    return DEFAULT_VERSION


def worktree_dirty() -> bool:
    """仅检查本项目路径下的改动，兄弟项目的进行中改动不阻塞发布。"""
    result = run_git(
        "-c", "status.relativePaths=false", "status", "--porcelain", "--untracked-files=normal"
    )
    if result.returncode != 0:
        return True
    project_prefix = f"/{PROJECT_ROOT.name}/"
    for line in result.stdout.splitlines():
        path = line[3:].strip().strip('"')
        if " -> " in path:
            path = path.split(" -> ", 1)[1]
        normalized = "/" + path.replace("\\", "/").lstrip("/")
        if not normalized.startswith(project_prefix):
            continue
        if any(normalized.endswith(suffix) for suffix in GENERATED_SUFFIXES):
            continue
        return True
    return False


def render_release_icon() -> None:
    if not RENDER_ICON.exists():
        print(f"⚠️ 缺少图标渲染脚本: {RENDER_ICON}", file=sys.stderr)
        return
    result = subprocess.run(["uv", "run", str(RENDER_ICON)], cwd=str(PROJECT_ROOT))
    if result.returncode != 0:
        print("⚠️ 图标渲染脚本执行异常，继续使用现有图标", file=sys.stderr)


def four_part_version(version: str) -> str:
    core = version.lstrip("v").split("-")[0]
    parts = (core.split(".") + ["0", "0", "0"])[:4]
    return ".".join(parts)


def build_resources(release: bool, gui: bool, out_exe: Path, version: str) -> int:
    if not WINRES_EXE.exists():
        print(f"❌ 缺少 go-winres: {WINRES_EXE}", file=sys.stderr)
        print("   安装：go install github.com/tc-hib/go-winres@latest", file=sys.stderr)
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
        result = subprocess.run(
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

    if result.returncode != 0:
        print(f"❌ go-winres 生成 syso 失败（exit {result.returncode}）", file=sys.stderr)
        return result.returncode

    kind = "GUI" if gui else "CLI"
    print(f"✅ [{kind}] PE 资源已注入: VERSIONINFO={fixed_version} icon={icon.name} → {SYSO_OUT.name}")
    return 0


def main() -> int:
    setup_console()

    parser = argparse.ArgumentParser(description="构建 mail2feishu 双通道二进制（dev / release 严格隔离）")
    parser.add_argument("--release", action="store_true", help="声明发布构建；tag 或工作区条件不满足时报错")
    parser.add_argument("--version", default="", help="覆盖版本基线（不改变 dev/release 判定）")
    args = parser.parse_args()

    exact_tag = exact_project_tag()
    dirty = worktree_dirty()
    release = exact_tag is not None and not dirty

    if args.release and not release:
        reason = "当前 HEAD 未命中 mail2feishu/v* tag" if exact_tag is None else "工作区存在未提交改动"
        print(f"❌ 拒绝发布构建：{reason}（release 需同时满足 精确 tag + 工作区干净）", file=sys.stderr)
        return 2

    base = args.version.lstrip("v") if args.version else base_version()
    if release:
        version = f"v{base}"
    else:
        version = f"v{base}-dev" + ("-dirty" if dirty else "")
    build_mode = "release" if release else "dev"
    rev = run_git("rev-parse", "--short", "HEAD").stdout.strip() or "unknown"
    build_id = rev + ("-dirty" if dirty else "")

    print(f"📦 构建配置: Version={version}, Mode={build_mode} (tag={exact_tag or '-'}, dirty={dirty})")

    if release:
        render_release_icon()

    OUT_DIR.mkdir(parents=True, exist_ok=True)

    targets = (
        (EXE_GUI_RELEASE if release else EXE_GUI_DEV, True),
        (EXE_CLI_RELEASE if release else EXE_CLI_DEV, False),
    )
    for out_exe, gui in targets:
        rc = build_resources(release, gui, out_exe, version)
        if rc != 0:
            return rc

        flags = ["-w", "-s"]
        if gui:
            flags.append("-H windowsgui")
        flags.append(f"-X mail2feishu/internal/buildinfo.Version={version}")
        flags.append(f"-X mail2feishu/internal/buildinfo.Mode={build_mode}")
        flags.append(f"-X mail2feishu/internal/buildinfo.BuildID={build_id}")

        tags = [] if release else ["dev"]
        if not gui:
            tags.append("cli")

        go_cmd = ["go", "build", "-trimpath"]
        if tags:
            go_cmd += ["-tags", ",".join(tags)]
        go_cmd += [f"-ldflags={' '.join(flags)}", "-o", str(out_exe), MAIN_PKG]

        channel = "GUI" if gui else "Console"
        print(f"🔨 [{channel}] {out_exe.name}")
        result = subprocess.run(go_cmd, cwd=str(PROJECT_ROOT))
        if result.returncode != 0:
            print(f"❌ go build 失败（exit {result.returncode}）: {out_exe.name}", file=sys.stderr)
            return result.returncode

    for out_exe, _gui in targets:
        if release and "dev" in out_exe.stem:
            print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
            return 3
        if not release and not out_exe.stem.endswith("-dev"):
            print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
            return 3

    for out_exe, gui in targets:
        size_mb = out_exe.stat().st_size / (1024 * 1024)
        print(f"🎉 构建完成 [{'GUI' if gui else 'Console'}]: {out_exe.name} ({size_mb:.2f} MB)")

    for legacy in LEGACY_EXES:
        if not legacy.exists():
            continue
        try:
            legacy.unlink()
        except OSError as exc:
            print(f"⚠️ 无法清理历史产物（可能仍在运行）: {legacy.name} ({exc})", file=sys.stderr)
            continue
        print(f"🧹 已清理历史产物: {legacy.name}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
