# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""winstartupmanager 统一构建脚本（uv 单文件）。

使用说明：
    uv run build.py              # 自动判定模式：HEAD 严格命中项目 tag 且工作区
                                 # 干净 -> release；否则 -> dev
    uv run build.py --dev        # 强制 dev 构建（产物 build/winstartupmanager-dev.exe）
    uv run build.py --release    # 强制 release（条件不满足时直接报错退出）
    uv run build.py --help       # 显示本说明

模式约定（与 go_projects 构建规范一致）：
    dev     产物名 winstartupmanager-dev.exe，版本号附 -dev 后缀，工作区不干净再附
            -dirty；保留控制台窗口便于看日志；-tags dev 切换 dev 图标与通道标识。
    release 产物名 winstartupmanager.exe（严禁任何 dev 标识）；仅当 HEAD 严格命中
            项目 tag `winstartupmanager/v*` 且工作区干净时允许；
            -trimpath + -H windowsgui 隐藏控制台。

构建流程：
    1) 前置检查（node/npm、wails3 CLI、go-winres）
    2) npm install（frontend/node_modules 缺失时）与 wails3 generate bindings
    3) 前端构建（npm run build，dev 模式用 build:dev 不压缩便于调试）
    4) 图标检查（release 用 build/icon.ico，dev 用 build/icon_dev.ico，缺失时调
       scripts/render_icon.py 生成）
    5) go-winres 生成 PE 资源（图标 + VERSIONINFO），go build 后即删除
    6) go build 注入 internal/env.{Version,Commit,BuildMode}

每次变更本脚本时必须同步更新本使用说明。
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent
FRONTEND_DIR = PROJECT_ROOT / "frontend"
BUILD_DIR = PROJECT_ROOT / "build"

APP_NAME = "winstartupmanager"
TAG_PREFIX = f"{APP_NAME}/"
FALLBACK_VERSION = "v0.1.0"

COMPANY_NAME = "shihao"
LEGAL_COPYRIGHT = "© 2026 shihao"


def info(msg: str) -> None:
    print(f"[build] {msg}")


def fail(msg: str, code: int = 1) -> None:
    print(f"[build][错误] {msg}", file=sys.stderr)
    sys.exit(code)


def run(cmd: list[str], **kw) -> None:
    resolved = shutil.which(cmd[0])
    if resolved:
        cmd = [resolved, *cmd[1:]]
    info("$ " + " ".join(cmd))
    subprocess.run(cmd, check=True, **kw)


def git(*args: str) -> str:
    return subprocess.run(
        ["git", *args], capture_output=True, text=True, encoding="utf-8", errors="replace"
    ).stdout.strip()


def detect_mode(force: str | None) -> tuple[str, str]:
    match = f"{TAG_PREFIX}v*"
    exact = git("describe", "--exact-match", "--tags", "--match", match, "HEAD")
    describe = git("describe", "--tags", "--long", "--match", match, "HEAD")
    dirty = bool(git("status", "--porcelain"))

    base = FALLBACK_VERSION
    if describe and describe.count("-") >= 2:
        tag = describe.rsplit("-", 2)[0]
        base = tag.rsplit("/", 1)[-1] if "/" in tag else tag

    if force == "release":
        if not exact:
            fail("release 要求 HEAD 精确命中项目 tag（winstartupmanager/v*，当前不满足）", code=2)
        if dirty:
            fail("release 要求工作区干净（存在未提交改动）", code=2)
        return "release", exact.rsplit("/", 1)[-1]

    if force is None and exact and not dirty:
        return "release", exact.rsplit("/", 1)[-1]

    version = f"{base}-dev"
    if dirty:
        version += "-dirty"
    return "dev", version


def numeric_version(version: str) -> str:
    nums = []
    for part in version.lstrip("v").split("-")[0].split("."):
        nums.append(int(part) if part.isdigit() else 0)
    nums = (nums + [0, 0, 0, 0])[:4]
    return ".".join(str(n) for n in nums)


def ensure_frontend() -> None:
    if not (FRONTEND_DIR / "node_modules").is_dir():
        run(["npm", "install"], cwd=FRONTEND_DIR)
    run(["wails3", "generate", "bindings"], cwd=PROJECT_ROOT)


def build_frontend(mode: str) -> None:
    script = "build:dev" if mode == "dev" else "build"
    run(["npm", "run", script], cwd=FRONTEND_DIR)


def ensure_icon(mode: str) -> Path:
    """release 用项目专属图标，dev 用灰蓝 DEV 图标（静态入库，缺失时按需生成）。"""
    if mode == "dev":
        ico = BUILD_DIR / "icon_dev.ico"
        if not ico.is_file():
            run(["uv", "run", "scripts/render_icon.py", "--dev"], cwd=PROJECT_ROOT)
        return ico
    ico = BUILD_DIR / "icon.ico"
    if not ico.is_file():
        run(["uv", "run", "scripts/render_icon.py"], cwd=PROJECT_ROOT)
    return ico


def make_syso(mode: str, version: str) -> tuple[Path, Path]:
    """用 go-winres 生成 PE 资源（图标 + VERSIONINFO），返回 (syso, 临时 json)。"""
    if not shutil.which("go-winres"):
        fail("找不到 go-winres，请执行 go install github.com/tc-hib/go-winres@latest")

    ico = ensure_icon(mode)
    ico_rel = ico.relative_to(PROJECT_ROOT).as_posix()
    fixed = numeric_version(version)
    display_version = version.lstrip("v")
    out_name = f"{APP_NAME}-dev.exe" if mode == "dev" else f"{APP_NAME}.exe"

    winres = {
        "RT_GROUP_ICON": {"APP": {"0000": ico_rel}},
        "RT_VERSION": {
            "#1": {
                "0000": {
                    "fixed": {"file_version": fixed, "product_version": fixed},
                    "info": {
                        "0409": {
                            "CompanyName": COMPANY_NAME,
                            "FileDescription": "WinStartupManager - 开机自启可视化管理工具",
                            "FileVersion": display_version,
                            "InternalName": APP_NAME,
                            "LegalCopyright": LEGAL_COPYRIGHT,
                            "OriginalFilename": out_name,
                            "ProductName": "WinStartupManager",
                            "ProductVersion": display_version,
                        }
                    },
                }
            }
        },
    }

    json_path = PROJECT_ROOT / "winres.generated.json"
    json_path.write_text(json.dumps(winres, ensure_ascii=False, indent=2), encoding="utf-8")
    run(["go-winres", "make", "--in", json_path.name, "--out", "rsrc", "--arch", "amd64"], cwd=PROJECT_ROOT)
    return PROJECT_ROOT / "rsrc_windows_amd64.syso", json_path


def go_build(mode: str, version: str) -> Path:
    commit = git("rev-parse", "--short", "HEAD") or "unknown"
    build_mode = "dev" if mode == "dev" else "release"
    ld = (
        f"-X winstartupmanager/internal/env.Version={version} "
        f"-X winstartupmanager/internal/env.Commit={commit} "
        f"-X winstartupmanager/internal/env.BuildMode={build_mode}"
    )
    out = BUILD_DIR / (f"{APP_NAME}-dev.exe" if mode == "dev" else f"{APP_NAME}.exe")
    cmd = ["go", "build", "-trimpath"]
    if mode == "release":
        cmd += ["-ldflags", f"-s -w -H windowsgui {ld}"]
    else:
        cmd += ["-tags", "dev", "-ldflags", ld]
    cmd += ["-o", str(out), "."]
    run(cmd, cwd=PROJECT_ROOT)
    return out


def main() -> None:
    parser = argparse.ArgumentParser(
        description="winstartupmanager 统一构建脚本（dev/release 自动判定，详见文件头说明）"
    )
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--dev", action="store_true", help="强制 dev 构建")
    group.add_argument("--release", action="store_true", help="强制 release 构建")
    args = parser.parse_args()
    force = "dev" if args.dev else ("release" if args.release else None)

    if not (FRONTEND_DIR / "package.json").is_file():
        fail(f"前端目录缺失: {FRONTEND_DIR}")
    if not os.environ.get("APPDATA"):
        info("警告：未检测到 APPDATA，运行时数据将回落 ~/.language_projects")

    mode, version = detect_mode(force)
    info(f"模式={mode} 版本={version}")

    ensure_frontend()
    build_frontend(mode)
    syso, winres_json = make_syso(mode, version)
    try:
        out = go_build(mode, version)
    finally:
        syso.unlink(missing_ok=True)
        winres_json.unlink(missing_ok=True)

    info(f"完成: {out}")


if __name__ == "__main__":
    main()
