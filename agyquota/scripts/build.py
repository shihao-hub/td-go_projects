# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow>=11.0"]
# ///
"""agyquota 双通道构建流水线。

产物规则（严格隔离，每条通道出两个二进制）：
  - dev（默认）：agyquota-gui-dev.exe（GUI 通道）+ agyquota-cli-dev.exe（Console 通道）
  - release：agyquota-gui.exe（GUI 通道）+ agyquota-cli.exe（Console 通道）
  只有 HEAD 精确命中 agyquota/v* tag 且工作区干净时才允许 release；
  dev 与 release 产物名不同，互不覆盖。

双通道设计：
  - GUI 通道用 -H windowsgui 构建，双击零黑框；它不承担 CLI 语义：
    任何启动方式都只显示配额面板，唯一语义参数是 --hidden（开机自启静默托盘）；
  - Console 通道为 console 子系统，父 shell 正常等待，
    prompt / 光标 / 管道 / 输出重定向全合规，终端、脚本、MCP 与 Agent 一律用它。

构建内容：
  - 图标按通道区分：release 用配额仪表图标，dev 用 Go 地鼠图标
  - release 构建前重绘专属图标并同步 internal/app/icon.ico
  - rsrc 将 PE Manifest 与 Icon 注入 cmd/agyquota/rsrc_windows_amd64.syso
  - 注入 Version / BuildID / BuildMode 三个 ldflags（buildinfo 单一真源）

用法：
  uv run scripts/build.py
  uv run scripts/build.py --release
  uv run scripts/build.py --version 1.2.0
"""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import sys
import time
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
# 四个产物统一落 bin/；勿用 build/（Wails 资源目录）或 dist/（前端产物）。
OUT_DIR = PROJECT_ROOT / "bin"
TAG_PREFIX = "agyquota/"

VERSION_FILE = PROJECT_ROOT / "info.json"
RENDER_ICON = PROJECT_ROOT / "scripts" / "render_icon.py"
MANIFEST = PROJECT_ROOT / "wails.exe.manifest"
ICON_RELEASE = PROJECT_ROOT / "icon.ico"
ICON_DEV = PROJECT_ROOT / "internal" / "app" / "icon_dev.ico"
SYSO_OUT = PROJECT_ROOT / "cmd" / "agyquota" / "rsrc_windows_amd64.syso"

EXE_GUI_RELEASE = OUT_DIR / "agyquota-gui.exe"
EXE_GUI_DEV = OUT_DIR / "agyquota-gui-dev.exe"
EXE_CLI_RELEASE = OUT_DIR / "agyquota-cli.exe"
EXE_CLI_DEV = OUT_DIR / "agyquota-cli-dev.exe"

# 历史产物（旧单产物与迁移前根目录双通道产物），构建后自动清理，避免与新产物混淆。
LEGACY_EXES = (
    PROJECT_ROOT / "agyquota.exe",
    PROJECT_ROOT / "agyquota-dev.exe",
    PROJECT_ROOT / "agyquota-gui.exe",
    PROJECT_ROOT / "agyquota-gui-dev.exe",
    PROJECT_ROOT / "agyquota-cli.exe",
    PROJECT_ROOT / "agyquota-cli-dev.exe",
)

# 构建自身生成的未跟踪文件：不计入「工作区不干净」
GENERATED_SUFFIXES = (
    "/bin/agyquota-gui.exe",
    "/bin/agyquota-gui-dev.exe",
    "/bin/agyquota-cli.exe",
    "/bin/agyquota-cli-dev.exe",
    "/agyquota.exe",
    "/agyquota-dev.exe",
    "/cmd/agyquota/rsrc_windows_amd64.syso",
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
        encoding="utf-8",
        errors="replace",
        check=False,
    )


def exact_project_tag() -> str | None:
    """HEAD 上精确命中的项目 tag（形如 agyquota/v1.0.0）。"""
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


def describe() -> str:
    """项目级 describe：只匹配 agyquota/v* tag，避免串到同仓其他项目。"""
    r = run_git("describe", "--tags", "--always", "--dirty", "--match", f"{TAG_PREFIX}v*")
    if r.returncode == 0 and r.stdout.strip():
        return r.stdout.strip()
    return "dev"


def base_version() -> str:
    tag = latest_project_tag()
    if tag:
        return tag[len(TAG_PREFIX):].lstrip("v")
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


def find_rsrc() -> str | None:
    exe = shutil.which("rsrc")
    if exe:
        return exe
    r = subprocess.run(["go", "env", "GOPATH"], capture_output=True, text=True, check=False)
    if r.returncode == 0:
        candidate = Path(r.stdout.strip()) / "bin" / "rsrc.exe"
        if candidate.is_file():
            return str(candidate)
    return None


def build_resources(release: bool, rsrc: str) -> int:
    """release 重绘专属图标；按通道选择 PE 图标并编译资源段。"""
    if release and RENDER_ICON.exists():
        r = subprocess.run(["uv", "run", str(RENDER_ICON)], cwd=str(PROJECT_ROOT))
        if r.returncode != 0:
            print("❌ 图标渲染脚本执行失败", file=sys.stderr)
            return r.returncode

    icon = ICON_RELEASE if release else ICON_DEV
    if not icon.exists():
        print(f"❌ 缺少图标文件: {icon}", file=sys.stderr)
        return 4

    r = subprocess.run(
        [
            rsrc,
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

    parser = argparse.ArgumentParser(description="构建 agyquota 双通道二进制（dev / release 严格隔离）")
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
    rsrc = find_rsrc()
    if not rsrc:
        print("❌ 未找到 rsrc：go install github.com/akavel/rsrc@latest 并确认 %GOPATH%\\bin 在 PATH", file=sys.stderr)
        return 4
    rc = build_resources(release, rsrc)
    if rc != 0:
        return rc

    base = args.version.lstrip("v") if args.version else base_version()
    version = f"v{base}" if release else f"v{base}-dev" + ("-dirty" if dirty else "")
    build_mode = "release" if release else "dev"
    # buildID = describe + Unix 秒时间戳：区分 dirty 状态下的多次构建（daemon 握手用）
    build_id = f"{describe()}.{int(time.time())}"

    print(f"📦 构建配置: Version={version}, BuildMode={build_mode}, BuildID={build_id} (tag={exact_tag or '-'}, dirty={dirty})")

    # 3. 逐通道 go build（每通道两个产物：GUI 零黑框 + Console 终端友好）
    OUT_DIR.mkdir(parents=True, exist_ok=True)   # go build 不会自动建父目录

    targets = (
        (EXE_GUI_RELEASE if release else EXE_GUI_DEV, True),
        (EXE_CLI_RELEASE if release else EXE_CLI_DEV, False),
    )
    for out_exe, gui in targets:
        flags = ["-w", "-s"]
        if gui:
            flags.append("-H windowsgui")
        flags.append(f"-X agyquota/internal/buildinfo.Version={version}")
        flags.append(f"-X agyquota/internal/buildinfo.BuildID={build_id}")
        flags.append(f"-X agyquota/internal/buildinfo.BuildMode={build_mode}")

        tags = [] if release else ["dev"]
        if not gui:
            tags.append("cli")

        go_cmd = ["go", "build", "-trimpath"]
        if tags:
            go_cmd += ["-tags", ",".join(tags)]
        go_cmd += [f"-ldflags={' '.join(flags)}", "-o", str(out_exe), "./cmd/agyquota"]

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

    # 5. 清理历史命名的单产物（双通道拆分前的 agyquota[-dev].exe）
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
