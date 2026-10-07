# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""typeai-gui 统一构建脚本（uv 单文件）。

使用说明：
    uv run build.py              # 自动判定模式：HEAD 严格命中 git tag 且工作区
                                 # 干净 → release；否则 → dev
    uv run build.py --dev        # 强制 dev 构建（产物 build/typeai-gui-dev.exe）
    uv run build.py --release    # 强制 release（条件不满足时直接报错退出）
    uv run build.py --help       # 显示本说明

模式约定（与 go_projects 构建规范一致）：
    dev     产物名 typeai-gui-dev.exe，版本号附 -dev 后缀，工作区不干净再附
            -dirty；保留控制台窗口便于看日志。
    release 产物名 typeai-gui.exe（严禁任何 dev 标识）；仅当 HEAD 严格命中
            git tag 且工作区干净时允许；-trimpath + -H windowsgui 隐藏控制台。

构建流程：
    1) 前置检查（node/npm、wails3 CLI、windres）
    2) npm install（frontend/node_modules 缺失时）与 wails3 generate bindings
    3) 前端构建（npm run build，dev 模式用 build:dev 不压缩便于调试）
    4) 图标检查（build/icon.ico 缺失时调 scripts/render_icon.py 生成）
    5) windres 生成 .syso（PE 图标与版本资源），go build 后即删除
    6) go build 注入 version.Version / version.Commit

每次变更本脚本时必须同步更新本使用说明。
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent
FRONTEND_DIR = PROJECT_ROOT / "frontend"
BUILD_DIR = PROJECT_ROOT / "build"

WINDRES_CANDIDATES = (
    r"D:\msys64\mingw64\bin\windres.exe",
    r"C:\msys64\mingw64\bin\windres.exe",
    r"D:\msys64\ucrt64\bin\windres.exe",
    r"C:\msys64\ucrt64\bin\windres.exe",
)


def info(msg: str) -> None:
    print(f"[build] {msg}")


def fail(msg: str) -> None:
    print(f"[build][错误] {msg}", file=sys.stderr)
    sys.exit(1)


def run(cmd: list[str], **kw) -> None:
    # Windows 下 npm 等是 .cmd 脚本，CreateProcess 不解析 PATHEXT，须先 which 展开
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
    """返回 (mode, version)。release 仅在 HEAD 精确命中 tag 且工作区干净时成立。"""
    exact = git("describe", "--exact-match", "--tags", "HEAD")
    describe = git("describe", "--tags", "--long", "HEAD")  # 形如 v1.2.3-3-gabcdef
    dirty = bool(git("status", "--porcelain"))

    base = describe.rsplit("-", 2)[0] if describe and describe.count("-") >= 2 else "v0.0.0"
    # 子仓 tag 带项目前缀（如 typeai/v0.1.0），版本号只取斜杠后的语义版本
    base = base.rsplit("/", 1)[-1]

    if force == "release":
        if not exact:
            fail("release 要求 HEAD 精确命中 git tag（当前不满足）")
        if dirty:
            fail("release 要求工作区干净（存在未提交改动）")
        return "release", exact

    if force is None and exact and not dirty:
        return "release", exact

    version = f"{base}-dev"
    if dirty:
        version += "-dirty"
    return "dev", version


def find_windres() -> str:
    found = shutil.which("windres")
    if found:
        return found
    for cand in WINDRES_CANDIDATES:
        if Path(cand).is_file():
            return cand
    fail("找不到 windres（MSYS2 MinGW64）；请安装 MSYS2 或将其 bin 加入 PATH")


def ensure_frontend() -> None:
    npm = "npm"
    if not (FRONTEND_DIR / "node_modules").is_dir():
        run([npm, "install"], cwd=FRONTEND_DIR)
    run(["wails3", "generate", "bindings"], cwd=PROJECT_ROOT)


def build_frontend(mode: str) -> None:
    script = "build:dev" if mode == "dev" else "build"
    run(["npm", "run", script], cwd=FRONTEND_DIR)


def make_syso(version: str) -> Path:
    """生成 PE 资源 .syso（图标 + 版本信息），返回路径供 go build 链接。"""
    windres = find_windres()
    ico = BUILD_DIR / "icon.ico"
    if not ico.is_file():
        run(["uv", "run", "scripts/render_icon.py"], cwd=PROJECT_ROOT)

    nums = []
    for part in version.lstrip("v").split("-")[0].split("."):
        nums.append(int(part) if part.isdigit() else 0)
    nums = (nums + [0, 0, 0, 0])[:4]

    rc = f"""1 ICON "icon.ico"

1 VERSIONINFO
FILEVERSION {nums[0]},{nums[1]},{nums[2]},{nums[3]}
PRODUCTVERSION {nums[0]},{nums[1]},{nums[2]},{nums[3]}
BEGIN
  BLOCK "StringFileInfo"
  BEGIN
    BLOCK "040904b0"
    BEGIN
      VALUE "FileDescription", "typeai terminal shell (xterm.js + ConPTY)"
      VALUE "FileVersion", "{version}"
      VALUE "ProductName", "typeai-gui"
      VALUE "ProductVersion", "{version}"
    END
  END
  BLOCK "VarFileInfo"
  BEGIN
    VALUE "Translation", 0x0409, 1200
  END
END
"""
    rc_path = BUILD_DIR / "icon.rc"
    rc_path.write_text(rc, encoding="ascii")
    syso = PROJECT_ROOT / "icon_windows_amd64.syso"
    # rc 内相对路径按 windres 的 cwd 解析，故工作目录设为 build/（icon.ico 所在地）；
    # windres 预处理依赖同目录的 gcc，须将其 bin 注入子进程 PATH
    env = {**os.environ, "PATH": f"{Path(windres).parent}{os.pathsep}{os.environ.get('PATH', '')}"}
    run([windres, "-O", "coff", "-o", str(syso), rc_path.name], cwd=BUILD_DIR, env=env)
    return syso


def go_build(mode: str, version: str) -> Path:
    commit = git("rev-parse", "--short", "HEAD") or "unknown"
    ld = (
        f"-X typeai-gui/version.Version={version} "
        f"-X typeai-gui/version.Commit={commit}"
    )
    out = BUILD_DIR / (f"typeai-gui-{mode}.exe" if mode == "dev" else "typeai-gui.exe")
    cmd = ["go", "build", "-trimpath"]
    if mode == "release":
        cmd += ["-ldflags", f"-s -w -H windowsgui {ld}"]
    else:
        cmd += ["-ldflags", ld]
    cmd += ["-o", str(out), "."]
    run(cmd, cwd=PROJECT_ROOT)
    return out


def main() -> None:
    parser = argparse.ArgumentParser(
        description="typeai-gui 统一构建脚本（dev/release 自动判定，详见文件头说明）"
    )
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--dev", action="store_true", help="强制 dev 构建")
    group.add_argument("--release", action="store_true", help="强制 release 构建")
    args = parser.parse_args()
    force = "dev" if args.dev else ("release" if args.release else None)

    if not (FRONTEND_DIR / "package.json").is_file():
        fail(f"前端目录缺失: {FRONTEND_DIR}")

    mode, version = detect_mode(force)
    info(f"模式={mode} 版本={version}")

    ensure_frontend()
    build_frontend(mode)
    syso = make_syso(version)
    try:
        out = go_build(mode, version)
    finally:
        syso.unlink(missing_ok=True)

    info(f"完成: {out}")


if __name__ == "__main__":
    main()
