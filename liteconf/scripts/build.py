# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""liteconf 双通道构建：每条通道各出 GUI（-H windowsgui）与 Console（-tags cli）两个二进制。

产物规则（严格隔离，统一输出到 build/）：
  - dev（默认）：build/liteconf-gui-dev.exe + build/liteconf-cli-dev.exe
  - release：build/liteconf-gui.exe     + build/liteconf-cli.exe
只有 HEAD 精确命中 liteconf/v* tag 且工作区干净时才允许 release；
dev 与 release 产物名不同，互不覆盖。

用法：
  uv run scripts/build.py              # dev 构建
  uv run scripts/build.py --release    # 发布构建（条件不满足 exit 2）
  uv run scripts/build.py --version 0.2.0   # 覆盖版本基线，不改变通道判定
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
# 四个产物统一落 bin/；勿用 build/（Wails 资源目录）或 dist/（前端产物）。
OUT_DIR = PROJECT_ROOT / "bin"
TAG_PREFIX = "liteconf/"

# PE 资源工具：go-winres（rsrc 不支持 VERSIONINFO，exe 属性面板版本会全空）。
WINRES_EXE = shutil.which("go-winres") or str(
    Path(os.environ.get("GOPATH", str(Path.home() / "go"))) / "bin" / "go-winres.exe"
)
WINRES_DIR = PROJECT_ROOT / "winres"
VERSION_FILE = PROJECT_ROOT / "info.json"
MANIFEST = PROJECT_ROOT / "wails.exe.manifest"
ICON_RELEASE = PROJECT_ROOT / "icon.ico"
ICON_DEV = PROJECT_ROOT / "internal" / "app" / "icon_dev.ico"
RENDER_ICON = PROJECT_ROOT / "scripts" / "render_icon.py"
SYSO_OUT = PROJECT_ROOT / "cmd" / "liteconf" / "rsrc_windows_amd64.syso"

# 历史产物目录（迁移至 bin/ 前使用），仅用于清理旧 exe。
BUILD_DIR = PROJECT_ROOT / "build"
EXE_GUI_RELEASE = OUT_DIR / "liteconf-gui.exe"
EXE_GUI_DEV = OUT_DIR / "liteconf-gui-dev.exe"
EXE_CLI_RELEASE = OUT_DIR / "liteconf-cli.exe"
EXE_CLI_DEV = OUT_DIR / "liteconf-cli-dev.exe"

# 历史产物（旧单产物与迁移前根目录双通道产物），构建后自动清理，避免与新产物混淆。
LEGACY_EXES = (
    BUILD_DIR / "liteconf.exe",
    BUILD_DIR / "liteconf-server.exe",
    BUILD_DIR / "liteconf-server.exe~",
    PROJECT_ROOT / "liteconf.exe",
    PROJECT_ROOT / "liteconf-dev.exe",
    PROJECT_ROOT / "liteconf-gui.exe",
    PROJECT_ROOT / "liteconf-gui-dev.exe",
    PROJECT_ROOT / "liteconf-cli.exe",
    PROJECT_ROOT / "liteconf-cli-dev.exe",
    BUILD_DIR / "liteconf-gui.exe",
    BUILD_DIR / "liteconf-gui-dev.exe",
    BUILD_DIR / "liteconf-cli.exe",
    BUILD_DIR / "liteconf-cli-dev.exe",
)

# 构建自身生成的未跟踪文件：不计入「工作区不干净」。
GENERATED_SUFFIXES = (
    "/bin/liteconf-gui.exe",
    "/bin/liteconf-gui-dev.exe",
    "/bin/liteconf-cli.exe",
    "/bin/liteconf-cli-dev.exe",
    "/build/liteconf.exe",
    "/build/liteconf-server.exe",
    "/cmd/liteconf/rsrc_windows_amd64.syso",
    "/winres/.tmp-winres.json",
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
    """HEAD 上精确命中的项目 tag（形如 liteconf/v1.0.0）。"""
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


def render_release_icon() -> None:
    """重绘 release 专属图标（dev 用静态地鼠资产，无需重绘）。"""
    if not RENDER_ICON.exists():
        return
    r = subprocess.run(["uv", "run", str(RENDER_ICON)], cwd=str(PROJECT_ROOT))
    if r.returncode != 0:
        print("⚠️ 图标渲染脚本执行异常，跳过重绘", file=sys.stderr)


def four_part_version(version: str) -> str:
    """把 v0.2.0 / 0.2.0-dev 转成 VERSIONINFO 要求的 4 段数字。"""
    core = version.lstrip("v").split("-")[0]
    parts = (core.split(".") + ["0", "0", "0"])[:4]
    return ".".join(parts)


def build_resources(release: bool, gui: bool, out_exe: Path, version: str) -> int:
    """按产物通道生成 PE 资源段（VERSIONINFO + manifest + icon）。

    GUI 与 CLI 落在同一个 Go 包（cmd/liteconf），而一个包只能链接一份
    rsrc_windows_amd64.syso，所以必须在每个产物 go build 之前重新生成一次，
    这样两个 exe 才能拥有各自的 FileDescription。
    """
    if not Path(WINRES_EXE).exists():
        print(f"❌ 缺少 go-winres: {WINRES_EXE}", file=sys.stderr)
        print("   安装：go install github.com/tc-hib/go-winres@latest", file=sys.stderr)
        return 4

    icon = ICON_RELEASE if release else ICON_DEV
    if not icon.exists():
        print(f"❌ 缺少图标文件: {icon}", file=sys.stderr)
        print("   release 图标可用：uv run scripts/render_icon.py", file=sys.stderr)
        return 4

    template = WINRES_DIR / ("gui.json" if gui else "cli.json")
    if not template.exists():
        print(f"❌ 缺少资源模板: {template}", file=sys.stderr)
        return 4

    fixed_version = four_part_version(version)
    # go-winres 按「相对 JSON 所在目录」解析资源路径，故临时 JSON 必须放 winres/。
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
                str(WINRES_EXE),
                "make",
                "--in",
                str(winres_json),
                "--arch",
                "amd64",
                "--out",
                str(SYSO_OUT.parent / "rsrc"),
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


def main() -> int:
    setup_console()

    parser = argparse.ArgumentParser(description="构建 liteconf 双通道二进制（dev / release 严格隔离）")
    parser.add_argument("--release", action="store_true", help="声明发布构建；tag 或工作区条件不满足时报错")
    parser.add_argument("--version", default="", help="覆盖版本基线（不改变 dev/release 判定）")
    args = parser.parse_args()

    # 1. 判定构建类型：默认 dev，仅 精确 tag + 干净工作区 才 release
    exact_tag = exact_project_tag()
    dirty = worktree_dirty()
    release = exact_tag is not None and not dirty

    if args.release and not release:
        reason = "当前 HEAD 未命中项目 tag" if exact_tag is None else "工作区存在未提交改动"
        print(f"❌ 拒绝发布构建：{reason}（release 需同时满足 精确 tag + 工作区干净）", file=sys.stderr)
        return 2

    # 2. 版本基线（PE VERSIONINFO 要用，必须先算）
    base = args.version.lstrip("v") if args.version else base_version()
    version = f"v{base}" if release else f"v{base}-dev" + ("-dirty" if dirty else "")
    build_mode = "release" if release else "dev"

    print(f"📦 构建配置: Version={version}, Mode={build_mode} (tag={exact_tag or '-'}, dirty={dirty})")

    if release:
        render_release_icon()

    OUT_DIR.mkdir(parents=True, exist_ok=True)

    # 3. 逐通道 go build（每通道两个产物：GUI 零黑框 + Console 终端友好）
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
        flags.append(f"-X github.com/shihao-hub/liteconf/internal/version.Version={version}")
        flags.append(f"-X github.com/shihao-hub/liteconf/internal/version.BuildMode={build_mode}")

        tags = [] if release else ["dev"]
        if not gui:
            tags.append("cli")

        go_cmd = ["go", "build", "-trimpath"]
        if tags:
            go_cmd += ["-tags", ",".join(tags)]
        go_cmd += [f"-ldflags={' '.join(flags)}", "-o", str(out_exe), "./cmd/liteconf"]

        channel = "GUI" if gui else "Console"
        print(f"🔨 [{channel}] {out_exe.name}")
        r = subprocess.run(go_cmd, cwd=str(PROJECT_ROOT))
        if r.returncode != 0:
            print(f"❌ go build 失败（exit {r.returncode}）: {out_exe.name}", file=sys.stderr)
            return r.returncode

    # 4. 产物名守卫：dev 必须带 -dev 后缀，release 不得带
    for out_exe, _gui in targets:
        if release and "dev" in out_exe.stem:
            print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
            return 3
        if not release and not out_exe.stem.endswith("-dev"):
            print(f"❌ 产物命名违规: {out_exe}", file=sys.stderr)
            return 3

    for out_exe, gui in targets:
        size_mb = out_exe.stat().st_size / (1024 * 1024)
        print(f"🎉 构建完成 [{'GUI' if gui else 'Console'}]: {out_exe} ({size_mb:.2f} MB)")

    # 5. 清理历史命名的单产物（liteconf.exe / liteconf-server.exe）
    for legacy in LEGACY_EXES:
        if not legacy.exists():
            continue
        try:
            legacy.unlink()
        except OSError as exc:
            print(
                f"⚠️ 无法清理历史产物（可能仍在运行）: {legacy.relative_to(PROJECT_ROOT)} ({exc})",
                file=sys.stderr,
            )
            continue
        print(f"🧹 已清理历史产物: {legacy.relative_to(PROJECT_ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
