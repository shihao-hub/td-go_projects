# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
"""
========================================================================================
Visual Studio Code 任务栏/可执行文件图标定制与自动更新恢复脚本 (优化加固版)
========================================================================================
【优化点】
1. 深度进程与文件句柄占用检测：避免 VS Code 后台残留子进程导致 rcedit "Unable to commit changes"。
2. 写入重试机制：当文件处于短暂关闭释放中时，自动重试 3 次。
3. 官方原生零风险外壳刷新三连击（绝不 taskkill explorer）：
   - os.utime 触碰任务栏快捷方式
   - ie4uinit.exe -show 原生刷新外壳图标缓存
   - SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_FLUSH) 广播外壳变更通知
========================================================================================
"""

import argparse
import ctypes
import io
import os
import shutil
import subprocess
import sys
import time
from pathlib import Path

# 控制台 UTF-8 输出
if sys.platform == "win32":
    if hasattr(sys.stdout, "buffer"):
        sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "buffer"):
        sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")

DEFAULT_EXE_CANDIDATES = [
    Path(r"D:\Users\29580\AppData\Local\Programs\Microsoft VS Code\Code.exe"),
    Path(os.path.expandvars(r"%LOCALAPPDATA%\Programs\Microsoft VS Code\Code.exe")),
    Path(r"C:\Program Files\Microsoft VS Code\Code.exe"),
]

RCEDIT_DEFAULT_PATH = Path(r"D:\Users\language_projects_bin\rcedit.exe")
SCRIPT_DIR = Path(__file__).resolve().parent

DEFAULT_BLUE_ICO = SCRIPT_DIR / "vscode_dark_blue.ico"
DEFAULT_SILVER_ICO = SCRIPT_DIR / "vscode_dark_silver.ico"


def find_target_exe(custom_path: str | None) -> Path:
    if custom_path:
        p = Path(custom_path).resolve()
        if not p.is_file():
            raise FileNotFoundError(f"未找到指定的 EXE 文件: {p}")
        return p

    for cand in DEFAULT_EXE_CANDIDATES:
        if cand.is_file():
            return cand

    raise FileNotFoundError("未能自动定位 Code.exe，请使用 --target-exe 手动指定路径。")


def ensure_rcedit(custom_path: str | None) -> Path:
    if custom_path:
        p = Path(custom_path).resolve()
        if p.is_file():
            return p
        raise FileNotFoundError(f"指定的 rcedit 不存在: {p}")

    if RCEDIT_DEFAULT_PATH.is_file():
        return RCEDIT_DEFAULT_PATH

    path_which = shutil.which("rcedit") or shutil.which("rcedit-x64")
    if path_which:
        return Path(path_which)

    raise RuntimeError(f"未找到 rcedit.exe，请确保其位于 PATH 或 {RCEDIT_DEFAULT_PATH}")


def is_file_locked(filepath: Path) -> bool:
    """检查文件是否被其他进程独占占用"""
    if not filepath.exists():
        return False
    try:
        # 尝试以独占写模式打开测试
        with open(filepath, "r+b"):
            pass
        return False
    except (IOError, PermissionError):
        return True


def ensure_process_closed(exe_path: Path, kill: bool = False, max_wait_sec: int = 5) -> None:
    """全面检测并确保 VS Code 进程已退出，避免残留后台子进程占用 EXE"""
    exe_name = exe_path.name

    def has_running_proc():
        try:
            output = subprocess.check_output(
                ["tasklist", "/FI", f"IMAGENAME eq {exe_name}", "/FO", "CSV"],
                text=True,
                creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
            )
            return exe_name.lower() in output.lower()
        except Exception:
            return False

    if has_running_proc() or is_file_locked(exe_path):
        if kill:
            print(f"[*] 正在终止运行中的 {exe_name} 及其后台进程...")
            subprocess.run(["taskkill", "/F", "/IM", exe_name, "/T"], check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            time.sleep(0.5)
        else:
            print(f"[!] 检测到 {exe_name} 正在运行或被后台占用，正在等待其退出...")
            for _ in range(max_wait_sec * 2):
                if not has_running_proc() and not is_file_locked(exe_path):
                    break
                time.sleep(0.5)

    if is_file_locked(exe_path):
        if not kill:
            print(f"[-] 错误: {exe_name} 仍被占用，请彻底关闭 VS Code 或附加 --kill 参数自动清理。", file=sys.stderr)
            sys.exit(1)
        else:
            time.sleep(1)


def apply_icon_with_retry(rcedit_exe: Path, exe_path: Path, icon_path: Path, max_retries: int = 3) -> bool:
    """调用 rcedit 注入 PE 图标资源，内置重试与防文件占用机制"""
    for attempt in range(1, max_retries + 1):
        if is_file_locked(exe_path):
            print(f"[*] 第 {attempt} 次检测: 目标 EXE 文件暂时被锁定，等待 1 秒...")
            time.sleep(1)

        cmd = [str(rcedit_exe), str(exe_path), "--set-icon", str(icon_path)]
        res = subprocess.run(cmd, capture_output=True, text=True)
        if res.returncode == 0:
            print(f"[+] 图标成功写入 {exe_path.name}！")
            return True
        else:
            if "Unable to commit changes" in res.stderr and attempt < max_retries:
                print(f"[*] 检测到文件提交冲突 (尝试 {attempt}/{max_retries})，等待文件句柄释放...")
                time.sleep(1.2)
                continue
            print(f"[-] rcedit 执行失败 (code {res.returncode}): {res.stderr.strip()}", file=sys.stderr)
            return False
    return False


def refresh_windows_icon_cache() -> None:
    """官方原生安全外壳刷新组合拳（零风险、不杀 Explorer）"""
    print("[*] 正在刷新 Windows 任务栏与外壳图标缓存...")

    # 1. 触碰固定快捷方式时间戳
    taskbar_dir = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar"))
    if taskbar_dir.is_dir():
        for lnk in taskbar_dir.glob("*Code*.lnk"):
            try:
                os.utime(lnk, None)
                print(f"[+] 已刷新任务栏快捷方式时间戳: {lnk.name}")
            except Exception:
                pass

    # 2. 调用 Windows 系统自带外壳图标刷新器 ie4uinit
    try:
        subprocess.run(["ie4uinit.exe", "-show"], check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        print("[+] 已调用系统内置 ie4uinit 刷新图标位图缓存")
    except Exception:
        pass

    # 3. 广播 SHChangeNotify 外壳关联变更通知
    try:
        # SHCNE_ASSOCCHANGED = 0x08000000, SHCNF_FLUSH = 0x1000
        ctypes.windll.shell32.SHChangeNotify(0x08000000, 0x1000, 0, 0)
        print("[+] 已广播 Windows SHChangeNotify 外壳关联更新")
    except Exception as e:
        print(f"[-] SHChangeNotify 调用异常: {e}")


def main() -> int:
    parser = argparse.ArgumentParser(
        description="VS Code 任务栏/可执行文件图标修改与更新恢复工具 (优化加固版)",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--style", choices=["blue", "silver"], default="blue", help="图标风格 (默认 blue: 科技蓝微光, silver: 极客银白)")
    parser.add_argument("--custom-icon", type=str, help="自定义图标路径 (.ico)")
    parser.add_argument("--restore", action="store_true", help="从 .bak 备份恢复官方原始 EXE")
    parser.add_argument("--kill", action="store_true", help="如果 VS Code 正在运行或后台占用，强制关闭它")
    parser.add_argument("--target-exe", type=str, help="手动指定 Code.exe 路径")
    parser.add_argument("--rcedit-path", type=str, help="手动指定 rcedit.exe 路径")

    args = parser.parse_args()

    exe_path = find_target_exe(args.target_exe)
    print(f"[*] 目标可执行文件: {exe_path}")

    bak_path = exe_path.with_name(f"{exe_path.name}.bak")
    internal_icos = list(exe_path.parent.glob("*/resources/app/resources/win32/code.ico"))

    # 1. 还原模式
    if args.restore:
        ensure_process_closed(exe_path, kill=args.kill)
        if bak_path.is_file():
            shutil.copy2(bak_path, exe_path)
            print(f"[+] 已恢复官方原始 EXE: {exe_path}")
            for ico_p in internal_icos:
                ico_bak = ico_p.with_name("code.ico.bak")
                if ico_bak.is_file():
                    shutil.copy2(ico_bak, ico_p)
                    print(f"[+] 已恢复内部资源 code.ico: {ico_p}")
            refresh_windows_icon_cache()
            return 0
        else:
            print(f"[-] 错误: 未找到备份文件 {bak_path}", file=sys.stderr)
            return 1

    # 2. 补丁模式
    ensure_process_closed(exe_path, kill=args.kill)
    rcedit_exe = ensure_rcedit(args.rcedit_path)

    # 建立原始备份
    if not bak_path.is_file():
        shutil.copy2(exe_path, bak_path)
        print(f"[+] 已创建官方原始 EXE 安全备份: {bak_path}")
    else:
        print(f"[*] 检测到已存在 EXE 安全备份: {bak_path} (保留原备份)")

    for ico_p in internal_icos:
        ico_bak = ico_p.with_name("code.ico.bak")
        if not ico_bak.is_file():
            shutil.copy2(ico_p, ico_bak)
            print(f"[+] 已创建内部资源 code.ico 备份: {ico_bak}")

    # 确定目标图标
    if args.custom_icon:
        target_ico = Path(args.custom_icon).resolve()
    elif args.style == "silver":
        target_ico = DEFAULT_SILVER_ICO
    else:
        target_ico = DEFAULT_BLUE_ICO

    if not target_ico.is_file():
        print(f"[-] 错误: 指定的图标不存在: {target_ico}", file=sys.stderr)
        return 1

    print(f"[*] 使用定制图标: {target_ico.name} ({target_ico})")

    # 执行注入 (带重试机制)
    print(f"[*] 正在调用 rcedit 注入 PE 图标资源: {exe_path.name}")
    if not apply_icon_with_retry(rcedit_exe, exe_path, target_ico):
        return 1

    # 同步覆写内部 win32/code.ico 资源
    for ico_p in internal_icos:
        try:
            shutil.copy2(target_ico, ico_p)
            print(f"[+] 已同步覆写内部资源图标: {ico_p}")
        except Exception as e:
            print(f"[-] 覆写内部资源异常: {e}")

    # 原生安全刷新
    refresh_windows_icon_cache()

    print("\n" + "=" * 60)
    print("[SUCCESS] VS Code 任务栏与可执行文件图标补丁注入完成！")
    print(f"• 目标文件: {exe_path}")
    print(f"• 原始备份: {bak_path}")
    print(f"• 当前图标风格: {args.style} ({target_ico.name})")
    print("=" * 60)
    return 0


if __name__ == "__main__":
    sys.exit(main())
