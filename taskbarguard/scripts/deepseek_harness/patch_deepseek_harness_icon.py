# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
"""
========================================================================================
DeepSeek Harness 任务栏/托盘/可执行文件图标精细化修改与自动更新恢复脚本 (优化加固版)
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
import urllib.request
from pathlib import Path
from PIL import Image

if sys.platform == "win32":
    if hasattr(sys.stdout, "buffer"):
        sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "buffer"):
        sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")

DEFAULT_EXE_CANDIDATES = [
    Path(r"D:\Users\29580\AppData\Local\Programs\DeepSeek Harness\DeepSeek Harness.exe"),
    Path(os.path.expandvars(r"%LOCALAPPDATA%\Programs\DeepSeek Harness\DeepSeek Harness.exe")),
]

RCEDIT_DEFAULT_PATH = Path(r"D:\Users\language_projects_bin\rcedit.exe")
RCEDIT_DOWNLOAD_URL = "https://github.com/electron/rcedit/releases/download/v2.0.0/rcedit-x64.exe"
ICO_SIZES = [16, 24, 32, 48, 64, 128, 256]


def find_target_exe(custom_path: str | None) -> Path:
    if custom_path:
        p = Path(custom_path).resolve()
        if not p.is_file():
            raise FileNotFoundError(f"未找到指定的 EXE 文件: {p}")
        return p

    for cand in DEFAULT_EXE_CANDIDATES:
        if cand.is_file():
            return cand

    raise FileNotFoundError("未能自动定位 DeepSeek Harness.exe，请使用 --target-exe 手动指定路径。")


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

    dest = RCEDIT_DEFAULT_PATH
    dest.parent.mkdir(parents=True, exist_ok=True)
    print(f"[*] 未检测到 rcedit 工具，正在自动从 GitHub 下载: {RCEDIT_DOWNLOAD_URL}")
    try:
        urllib.request.urlretrieve(RCEDIT_DOWNLOAD_URL, dest)
        print(f"[+] 下载完成: {dest}")
        return dest
    except Exception as exc:
        raise RuntimeError(f"下载 rcedit 失败: {exc}。请手动放置 rcedit.exe 到系统 PATH 或 {dest}")


def is_file_locked(filepath: Path) -> bool:
    if not filepath.exists():
        return False
    try:
        with open(filepath, "r+b"):
            pass
        return False
    except (IOError, PermissionError):
        return True


def ensure_process_closed(exe_path: Path, kill: bool = False, max_wait_sec: int = 5) -> None:
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
            print(f"[*] 正在自动终止运行中的 {exe_name} 进程...")
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
            print(f"[-] 错误: {exe_name} 仍被占用，请先退出应用或附加 --kill 参数自动清理。", file=sys.stderr)
            sys.exit(1)
        else:
            time.sleep(1)


def apply_icon_with_retry(rcedit_exe: Path, exe_path: Path, icon_path: Path, max_retries: int = 3) -> bool:
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
    print("[*] 正在安全刷新 Windows 任务栏与外壳图标缓存...")
    taskbar_dir = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar"))
    if taskbar_dir.is_dir():
        for lnk in taskbar_dir.glob("*DeepSeek*.lnk"):
            try:
                os.utime(lnk, None)
                print(f"[+] 已刷新任务栏快捷方式时间戳: {lnk.name}")
            except Exception:
                pass

    try:
        subprocess.run(["ie4uinit.exe", "-show"], check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        print("[+] 已调用系统内置 ie4uinit 刷新图标位图缓存")
    except Exception:
        pass

    try:
        ctypes.windll.shell32.SHChangeNotify(0x08000000, 0x1000, 0, 0)
        print("[+] 已广播 Windows SHChangeNotify 外壳关联更新")
    except Exception as e:
        print(f"[-] SHChangeNotify 调用异常: {e}")


def main() -> int:
    parser = argparse.ArgumentParser(
        description="DeepSeek Harness 任务栏/托盘/可执行文件图标修改与更新恢复工具 (优化加固版)",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--custom-icon", type=str, help="自定义图标路径 (.ico)")
    parser.add_argument("--restore", action="store_true", help="从 .bak 备份恢复官方原始 EXE 及托盘图标")
    parser.add_argument("--kill", action="store_true", help="如果应用正在运行或占用，强制关闭")
    parser.add_argument("--target-exe", type=str, help="手动指定 DeepSeek Harness.exe 路径")
    parser.add_argument("--rcedit-path", type=str, help="手动指定 rcedit.exe 路径")
    parser.add_argument("--no-tray", action="store_true", help="不修改右下角托盘图标")

    args = parser.parse_args()

    exe_path = find_target_exe(args.target_exe)
    print(f"[*] 目标可执行文件: {exe_path}")

    bak_path = exe_path.with_name(f"{exe_path.name}.bak")
    tray_ico_path = exe_path.parent / "resources" / "tray.ico"
    tray_bak_path = exe_path.parent / "resources" / "tray.ico.bak"

    if args.restore:
        ensure_process_closed(exe_path, kill=args.kill)
        restored_any = False
        if bak_path.is_file():
            shutil.copy2(bak_path, exe_path)
            print(f"[+] 已恢复官方原始 EXE: {exe_path}")
            restored_any = True
        if tray_bak_path.is_file():
            shutil.copy2(tray_bak_path, tray_ico_path)
            print(f"[+] 已恢复官方原始托盘图标: {tray_ico_path}")
            restored_any = True
        if restored_any:
            refresh_windows_icon_cache()
            return 0
        else:
            print("[-] 错误: 未找到备份文件，无法还原！", file=sys.stderr)
            return 1

    ensure_process_closed(exe_path, kill=args.kill)
    rcedit_exe = ensure_rcedit(args.rcedit_path)

    if not bak_path.is_file():
        shutil.copy2(exe_path, bak_path)
        print(f"[+] 已创建官方原始 EXE 安全备份: {bak_path}")
    else:
        print(f"[*] 检测到已存在 EXE 安全备份: {bak_path} (保留原备份)")

    if tray_ico_path.is_file() and not tray_bak_path.is_file():
        shutil.copy2(tray_ico_path, tray_bak_path)
        print(f"[+] 已创建官方原始托盘图标安全备份: {tray_bak_path}")

    # 确定图标
    if args.custom_icon:
        target_ico = Path(args.custom_icon).resolve()
    else:
        target_ico = SCRIPT_DIR / "custom_icons" / "3_dark_monochrome_centered.ico"

    if not target_ico.is_file():
        print(f"[-] 错误: 指定的图标不存在: {target_ico}", file=sys.stderr)
        return 1

    print(f"[*] 使用定制图标: {target_ico.name} ({target_ico})")

    # 写入 PE (重试加固)
    print(f"[*] 正在调用 rcedit 注入 PE 图标资源: {exe_path.name}")
    if not apply_icon_with_retry(rcedit_exe, exe_path, target_ico):
        return 1

    if not args.no_tray:
        try:
            shutil.copy2(target_ico, tray_ico_path)
            print(f"[+] 已同步更新右下角托盘图标: {tray_ico_path}")
        except Exception as e:
            print(f"[-] 托盘图标写入异常: {e}")

    refresh_windows_icon_cache()

    print("\n" + "=" * 60)
    print("[SUCCESS] DeepSeek Harness 任务栏与托盘图标补丁完成！")
    print(f"• 目标文件: {exe_path}")
    print(f"• 原始备份: {bak_path}")
    print(f"• 当前图标: {target_ico.name}")
    print("=" * 60)
    return 0


if __name__ == "__main__":
    sys.exit(main())
