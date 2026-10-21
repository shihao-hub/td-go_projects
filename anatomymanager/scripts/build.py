# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""Anatomy Manager 单文件二进制构建流水线。

产物规则（严格隔离，每条通道出两个二进制）：
  - dev（默认）：anatomymanager-dev.exe（GUI 通道）+ anatomymanager-cli-dev.exe（Console 通道）
  - release：anatomymanager.exe（GUI 通道）+ anatomymanager-cli.exe（Console 通道）
  只有 HEAD 精确命中 anatomymanager/v* tag 且工作区干净时才允许 release；
  dev 与 release 产物名不同，互不覆盖。

双通道设计（为何不能只出一个）：
  - GUI 通道用 -H windowsgui 构建，双击零黑框，但父 shell（PowerShell / cmd）
    对 GUI 子系统进程不等待，prompt 会先于程序输出渲染，终端里跑 CLI 必然出现
    「输出黏在 prompt 行 + 末尾光标悬空」；
  - Console 通道为 console 子系统，shell 正常等待，prompt / 光标 / 管道 / 重定向全合规，
    终端、脚本与 Agent 一律用它。

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
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
TAG_PREFIX = "anatomymanager/"
RSRC_EXE = Path(r"C:\Users\29580\go\bin\rsrc.exe")

VERSION_FILE = PROJECT_ROOT / "info.json"
MANIFEST = PROJECT_ROOT / "wails.exe.manifest"
ICON_RELEASE = PROJECT_ROOT / "icon.ico"
ICON_DEV = PROJECT_ROOT / "internal" / "app" / "icon_dev.ico"
SYSO_OUT = PROJECT_ROOT / "cmd" / "anatomymanager" / "rsrc_windows_amd64.syso"
EXE_RELEASE = PROJECT_ROOT / "anatomymanager.exe"
EXE_DEV = PROJECT_ROOT / "anatomymanager-dev.exe"
EXE_CLI_RELEASE = PROJECT_ROOT / "anatomymanager-cli.exe"
EXE_CLI_DEV = PROJECT_ROOT / "anatomymanager-cli-dev.exe"
RENDER_ICON = PROJECT_ROOT / "scripts" / "render_icon.py"

# 构建自身生成的未跟踪文件：不计入「工作区不干净」
GENERATED_SUFFIXES = (
    "/anatomymanager.exe",
    "/anatomymanager-dev.exe",
    "/anatomymanager-cli.exe",
    "/anatomymanager-cli-dev.exe",
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


def build_resources(release: bool) -> int:
    """重绘 release 图标并编译 PE 资源段；dev 使用静态地鼠图标。"""
    if RENDER_ICON.exists():
        r = subprocess.run(["uv", "run", str(RENDER_ICON)], cwd=str(PROJECT_ROOT))
        if r.returncode != 0:
            print("⚠️ 图标渲染脚本执行异常，跳过重绘", file=sys.stderr)

    icon = ICON_RELEASE if release else ICON_DEV
    if not icon.exists():
        print(f"❌ 缺少图标文件: {icon}", file=sys.stderr)
        return 4

    r = subprocess.run(
        [
            str(RSRC_EXE),
            "-manifest", str(MANIFEST),
            "-ico", str(icon),
            "-arch", "amd64",
            "-o", str(SYSO_OUT),
        ],
        cwd=str(PROJECT_ROOT),
    )
    if r.returncode != 0:
        print(f"❌ rsrc 编译 syso 失败（exit {r.returncode}）", file=sys.stderr)
        return r.returncode
    print(f"✅ 已注入 Windows PE 资源段: {SYSO_OUT} (icon: {icon.name})")
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

    # 2. 图标与 PE 资源（按通道选择图标）
    rc = build_resources(release)
    if rc != 0:
        return rc

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

    # 3. 逐通道 go build（每通道两个产物：GUI 零黑框 + Console 终端友好）
    targets = (
        (EXE_RELEASE if release else EXE_DEV, True),
        (EXE_CLI_RELEASE if release else EXE_CLI_DEV, False),
    )
    for out_exe, gui in targets:
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
    return 0


if __name__ == "__main__":
    sys.exit(main())
