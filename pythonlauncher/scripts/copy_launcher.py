# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# copy_launcher.py —— 交互式把 pythonlauncher 构建产物 launcher.exe 复制到指定目录
# 用法：uv run scripts/copy_launcher.py <目标目录>
# 交互流程：三选一确定 exe 名字（[1] 目标目录名.exe / [2] 自定义 / [3] launcher.exe）→
#           目标已存在时询问 overwrite [y/N] → 复制并打印落位路径
# 退出码：0=成功；1=执行失败（目录不存在/取消/复制出错）；2=用法错误
# 说明：项目根按本脚本位置上溯定位（scripts/ 的上一级），与调用目录无关；
#       launcher.exe 由 `uv run build.py` 构建，本脚本不负责构建。

import shutil
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent

if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() not in ("utf-8", "utf8"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


def fail(message: str) -> int:
    print(f"copy_launcher: {message}", file=sys.stderr)
    return 1


def locate_launcher_exe() -> Path:
    exe = PROJECT_ROOT / "launcher.exe"
    if not exe.exists():
        raise FileNotFoundError(f"launcher.exe not found: {exe} (run 'uv run build.py' first)")
    if exe.is_dir():
        raise IsADirectoryError(f"launcher.exe is a directory: {exe}")
    return exe


def ask_name(target_dir: Path) -> str:
    """交互确定目标文件名：[1] 目标目录名 [2] 自定义 [3] launcher.exe（默认）"""
    dir_name = target_dir.name
    while True:
        print(f"exe 名字：[1] {dir_name}.exe（目录名）  [2] 自定义  [3] launcher.exe（默认）")
        choice = input("选择 [1/2/3]: ").strip()
        if choice == "1":
            return f"{dir_name}.exe"
        if choice == "2":
            while True:
                name = input("输入名字（带不带 .exe 均可）: ").strip()
                if not name:
                    print("名字不能为空，请重新输入")
                    continue
                if not name.lower().endswith(".exe"):
                    name += ".exe"
                return name
        if choice == "3":
            return "launcher.exe"
        print("无效选择，请输入 1 / 2 / 3")


def main() -> int:
    args = sys.argv[1:]
    if len(args) != 1:
        print("usage: uv run scripts/copy_launcher.py <目标目录>", file=sys.stderr)
        return 2
    target_dir = Path(args[0])

    if not target_dir.is_dir():
        return fail(f"target directory not found: {target_dir}")

    try:
        name = ask_name(target_dir)
    except EOFError:
        return fail("input stream closed")

    dst = target_dir / name

    if dst.exists():
        ans = input(f"{dst} 已存在，overwrite? [y/N]: ")
        if ans.strip().lower() != "y":
            return fail("canceled")

    try:
        src = locate_launcher_exe()
        shutil.copyfile(src, dst)
    except OSError as exc:
        return fail(str(exc))

    print(f"copied: {dst}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
