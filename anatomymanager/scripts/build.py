# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""Anatomy Manager 单文件二进制构建流水线。

产物规则（严格隔离，每条通道出两个二进制，命名固定为 `anatomymanager-<通道>[-dev].exe`）：
  - dev（默认）：anatomymanager-gui-dev.exe（GUI 通道）+ anatomymanager-cli-dev.exe（Console 通道）
  - release：anatomymanager-gui.exe（GUI 通道）+ anatomymanager-cli.exe（Console 通道）
  只有 HEAD 精确命中 anatomymanager/v* tag 且工作区干净时才允许 release；
  dev 与 release 产物名不同，互不覆盖。

双通道设计（为何不能只出一个）：
  - GUI 通道用 -H windowsgui 构建，双击零黑框；它**不承担 CLI 语义**：
    不管从哪里启动（双击 / 终端 / 快捷方式 / 带任意参数）结果都是显示 GUI，
    唯一语义参数是 --hidden（开机自启的静默托盘），其余参数一律忽略；
  - Console 通道为 console 子系统，父 shell 正常等待，
    prompt / 光标 / 管道 / 输出重定向全合规，终端、脚本与 Agent 一律用它。

为何不保留「单产物双模」（GUI 子系统 + AttachConsole 抢父控制台）：
  PowerShell / cmd 对 GUI 子系统进程不等待，prompt 先于程序输出渲染，
  输出必顶在 prompt 行上、末尾光标悬空；且 PowerShell 不向 GUI 进程传管道句柄，
  `> out.json` 会拿到空文件。业内（git / git-gui、docker / Docker Desktop、
  python / pythonw）一律拆成两个产物。

构建内容：
  - 图标按通道区分：release 用专属精密机械图标，dev 用 Go 地鼠图标
    （exe 文件图标与窗口/托盘图标保持一致）
  - 自动渲染/更新 Windows 多尺寸专属图标
  - 调用 rsrc 将 PE Manifest 与 Icon 资源编译注入 cmd/anatomymanager/*.syso
  - GUI 通道：静态链接、去符号、-H windowsgui 零黑框
  - Console 通道：静态链接、去符号、console 子系统（-tags cli）

用法：
  uv run scripts/build.py              # 开发构建 (dev)
  uv run scripts/build.py --release    # 发布构建（条件不满足时直接报错退出）
  uv run scripts/build.py --version 1.2.0   # 仅覆盖版本基线，不改变 dev/release 判定
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
# 四个产物统一落 bin/；勿用 build/（Wails 资源目录）或 dist/（前端产物）。
OUT_DIR = PROJECT_ROOT / "bin"
TAG_PREFIX = "anatomymanager/"

# PE 资源工具：go-winres（rsrc 不支持 VERSIONINFO，导致 exe 属性面板版本全空）。
# 可通过环境变量 GO_WINRES 覆盖路径。
WINRES_EXE = Path(os.environ.get("GO_WINRES") or r"C:\Users\29580\go\bin\go-winres.exe")
WINRES_DIR = PROJECT_ROOT / "winres"

VERSION_FILE = PROJECT_ROOT / "info.json"
MANIFEST = PROJECT_ROOT / "wails.exe.manifest"
ICON_RELEASE = PROJECT_ROOT / "icon.ico"
ICON_DEV = PROJECT_ROOT / "internal" / "app" / "icon_dev.ico"
SYSO_OUT = PROJECT_ROOT / "cmd" / "anatomymanager" / "rsrc_windows_amd64.syso"
EXE_GUI_RELEASE = OUT_DIR / "anatomymanager-gui.exe"
EXE_GUI_DEV = OUT_DIR / "anatomymanager-gui-dev.exe"
EXE_CLI_RELEASE = OUT_DIR / "anatomymanager-cli.exe"
EXE_CLI_DEV = OUT_DIR / "anatomymanager-cli-dev.exe"

# 历史产物（旧单产物与迁移前根目录双通道产物），构建后自动清理，避免与新产物混淆。
LEGACY_EXES = (
    PROJECT_ROOT / "anatomymanager.exe",
    PROJECT_ROOT / "anatomymanager-dev.exe",
    PROJECT_ROOT / "anatomymanager-gui.exe",
    PROJECT_ROOT / "anatomymanager-gui-dev.exe",
    PROJECT_ROOT / "anatomymanager-cli.exe",
    PROJECT_ROOT / "anatomymanager-cli-dev.exe",
)
RENDER_ICON = PROJECT_ROOT / "scripts" / "render_icon.py"

# 构建自身生成的未跟踪文件：不计入「工作区不干净」
GENERATED_SUFFIXES = (
    "/bin/anatomymanager-gui.exe",
    "/bin/anatomymanager-gui-dev.exe",
    "/bin/anatomymanager-cli.exe",
    "/bin/anatomymanager-cli-dev.exe",
    "/anatomymanager.exe",
    "/anatomymanager-dev.exe",
    "/cmd/anatomymanager/rsrc_windows_amd64.syso",
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


def render_release_icon() -> None:
    """重绘 release 图标（dev 用静态地鼠资产，无需重绘）。"""
    if RENDER_ICON.exists():
        r = subprocess.run(["uv", "run", str(RENDER_ICON)], cwd=str(PROJECT_ROOT))
        if r.returncode != 0:
            print("⚠️ 图标渲染脚本执行异常，跳过重绘", file=sys.stderr)


def four_part_version(version: str) -> str:
    """把 v1.2.1 / 1.2.1-dev 转成 VERSIONINFO 要求的 4 段数字。"""
    core = version.lstrip("v").split("-")[0]
    parts = (core.split(".") + ["0", "0", "0"])[:4]
    return ".".join(parts)


def build_resources(release: bool, gui: bool, out_exe: Path, version: str) -> int:
    """按产物通道生成 PE 资源段（VERSIONINFO + manifest + icon）。

    GUI 与 CLI 落在同一个 Go 包（cmd/anatomymanager），而一个包只能链接一份
    rsrc_windows_amd64.syso，所以必须在每个产物 go build 之前重新生成一次，
    这样才能让两个 exe 拥有各自的 FileDescription。
    """
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
    # go-winres 把 JSON 里的资源路径当作「相对 JSON 文件目录」解析，
    # 因此临时 JSON 必须放在 winres/ 下，路径用正斜杠相对路径（避免转义与拼错）。
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


def main() -> int:
    setup_console()

    parser = argparse.ArgumentParser(description="构建 anatomymanager 双通道二进制（dev / release 严格隔离）")
    parser.add_argument("--release", action="store_true", help="声明发布构建；tag 或工作区条件不满足时直接报错")
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

    # 2. 版本基线（必须在 PE 资源生成之前算好，VERSIONINFO 要用）
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

    # release 通道重绘专属图标（dev 用已入库的静态地鼠资产）
    if release:
        render_release_icon()

    # 3. 逐通道 go build（每通道两个产物：GUI 零黑框 + Console 终端友好）
    OUT_DIR.mkdir(parents=True, exist_ok=True)   # go build 不会自动建父目录

    targets = (
        (EXE_GUI_RELEASE if release else EXE_GUI_DEV, True),
        (EXE_CLI_RELEASE if release else EXE_CLI_DEV, False),
    )
    for out_exe, gui in targets:
        # 一个包只能链接一份 syso，所以每个产物构建前都重新生成带对应
        # FileDescription 的资源段（GUI / CLI 的属性描述必须不同）。
        rc = build_resources(release, gui, out_exe, version)
        if rc != 0:
            return rc

        flags = ["-w", "-s"]
        if gui:
            flags.append("-H windowsgui")
        flags.append(f"-X anatomymanager/internal/app.Version={version}")
        flags.append(f"-X anatomymanager/internal/app.BuildMode={build_mode}")

        tags = [] if release else ["dev"]
        if not gui:
            tags.append("cli")

        go_cmd = ["go", "build", "-trimpath"]
        if tags:
            go_cmd += ["-tags", ",".join(tags)]
        go_cmd += [f"-ldflags={' '.join(flags)}", "-o", str(out_exe), "./cmd/anatomymanager"]

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
        print(f"🎉 构建完成 [{'GUI' if gui else 'Console'}]: {out_exe.name} ({size_mb:.2f} MB)")

    # 5. 清理历史命名的单产物（双通道拆分前的 anatomymanager[-dev].exe）
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
