# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
r"""
========================================================================================
Antigravity 任务栏/托盘/可执行文件图标精细化修改与自动更新恢复脚本 (TaskbarGuard 官方加固版)
========================================================================================
【核心技术特性】
1. 运行中零感原子热替换 (Zero-kill Hot Swap)：
   - 突破 Windows 内核可执行文件写独占锁 (ERROR_ACCESS_DENIED)；
   - 支持在 Antigravity 运行中同分区原子重命名换入打过补丁的新 EXE，无需强杀正在工作的 IDE；
2. 任务栏钉选穿透 (Taskbar Pinned Shortcut Penetration)：
   - 深度扫描 User Pinned\TaskBar、开始菜单、桌面等全部快捷方式；
   - 同步显式重定向快捷方式 IconLocation 并广播 Windows 外壳变更；
3. 官方原版双向无损还原：
   - 自动建立 Antigravity.exe.bak 原始快照，--revert / --restore 100% 还原；
"""

from __future__ import annotations

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

if sys.platform == "win32":
    if hasattr(sys.stdout, "buffer"):
        sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "buffer"):
        sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")

SCRIPT_DIR = Path(__file__).resolve().parent

DEFAULT_EXE_CANDIDATES = [
    Path(r"C:\Users\29580\AppData\Local\Programs\Antigravity\Antigravity.exe"),
    Path(os.path.expandvars(r"%LOCALAPPDATA%\Programs\Antigravity\Antigravity.exe")),
    Path(r"D:\Users\29580\AppData\Local\Programs\Antigravity\Antigravity.exe"),
    Path(r"C:\Program Files\Antigravity\Antigravity.exe"),
]

RCEDIT_DEFAULT_PATH = Path(r"D:\Users\language_projects_bin\rcedit.exe")
RCEDIT_DOWNLOAD_URL = "https://github.com/electron/rcedit/releases/download/v2.0.0/rcedit-x64.exe"


def find_target_exe(custom_path: str | None) -> Path:
    if custom_path:
        p = Path(custom_path).resolve()
        if not p.is_file():
            raise FileNotFoundError(f"未找到指定的 EXE 文件: {p}")
        return p

    for cand in DEFAULT_EXE_CANDIDATES:
        if cand.is_file():
            return cand

    raise FileNotFoundError("未能自动定位 Antigravity.exe，请使用 --target-exe 手动指定路径。")


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


def apply_icon_to_file(rcedit_exe: Path, target_exe: Path, ico_path: Path, max_retries: int = 3) -> bool:
    for attempt in range(1, max_retries + 1):
        try:
            cmd = [str(rcedit_exe), str(target_exe), "--set-icon", str(ico_path)]
            proc = subprocess.run(cmd, capture_output=True, text=True, check=False)
            if proc.returncode == 0:
                print(f"[+] PE 图标资源注入成功")
                return True
            else:
                err_msg = proc.stderr.strip() or proc.stdout.strip()
                print(f"[-] rcedit 注入失败 (尝试 {attempt}/{max_retries}): {err_msg}", file=sys.stderr)
        except Exception as e:
            print(f"[-] 执行 rcedit 异常 (尝试 {attempt}/{max_retries}): {e}", file=sys.stderr)

        if attempt < max_retries:
            time.sleep(1.0)
    return False


def get_shortcut_target(lnk_path: Path) -> Path | None:
    """使用 PowerShell WScript.Shell 读取快捷方式的 TargetPath。"""
    try:
        ps_cmd = (
            f"$sh = New-Object -ComObject WScript.Shell; "
            f"$t = $sh.CreateShortcut('{str(lnk_path)}'); "
            f"[Console]::WriteLine($t.TargetPath)"
        )
        res = subprocess.run(
            ["powershell", "-NoProfile", "-NonInteractive", "-Command", ps_cmd],
            capture_output=True,
            text=True,
            check=False,
        )
        if res.returncode == 0:
            target_str = res.stdout.strip()
            if target_str:
                return Path(target_str)
    except Exception:
        pass
    return None


def update_shortcut_icon(lnk_path: Path, icon_location: str) -> bool:
    """使用 PowerShell WScript.Shell 穿透修改快捷方式的 IconLocation。"""
    try:
        ps_cmd = (
            f"$sh = New-Object -ComObject WScript.Shell; "
            f"$t = $sh.CreateShortcut('{str(lnk_path)}'); "
            f"$t.IconLocation = '{icon_location}'; "
            f"$t.Save()"
        )
        res = subprocess.run(
            ["powershell", "-NoProfile", "-NonInteractive", "-Command", ps_cmd],
            capture_output=True,
            text=True,
            check=False,
        )
        return res.returncode == 0
    except Exception:
        return False


EXCLUDED_LNK_KEYWORDS = ("manager", "tools", "ide", "helper", "updater", "uninstall", "setup")


def is_antigravity_shortcut(lnk: Path, target_exe: Path | None = None) -> bool:
    """严格判断快捷方式是否归属于 Antigravity 官方主程序。"""
    stem_lower = lnk.stem.lower()
    # 1. 过滤名字中包含 manager / tools / ide / updater 等周边独立工具的快捷方式
    if any(k in stem_lower for k in EXCLUDED_LNK_KEYWORDS):
        return False

    # 2. 读取快捷方式的实际 TargetPath
    actual_target = get_shortcut_target(lnk)
    if not actual_target:
        return False

    actual_name_lower = actual_target.name.lower()
    # 目标 EXE 文件名必须是 Antigravity 官方主程序 (排除 Antigravity IDE.exe 等)
    if actual_name_lower != "antigravity.exe":
        return False

    # 若指定了当前注入的 target_exe，严格校验绝对路径是否完全一致
    if target_exe:
        try:
            if actual_target.resolve() != target_exe.resolve():
                return False
        except Exception:
            if actual_name_lower != target_exe.name.lower():
                return False

    return True


def refresh_windows_icon_cache(
    target_exe: Path | None = None,
    icon_file_for_shortcuts: Path | None = None,
    restart_explorer: bool = False,
) -> None:
    print("[*] 正在穿透刷新 Windows 任务栏钉选项与快捷方式...")

    dirs_to_check = [
        # 任务栏固定项（核心！）
        Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar")),
        Path(os.path.expandvars(r"%APPDATA%\Microsoft\Windows\Start Menu\Programs")),
        Path(os.path.expandvars(r"%PROGRAMDATA%\Microsoft\Windows\Start Menu\Programs")),
        Path(os.path.expandvars(r"%USERPROFILE%\Desktop")),
        Path(os.path.expandvars(r"%PUBLIC%\Desktop")),
    ]

    for d in dirs_to_check:
        if not d.is_dir():
            continue
        for pat in ["*Antigravity*.lnk", "*antigravity*.lnk"]:
            for lnk in d.rglob(pat):
                try:
                    if not is_antigravity_shortcut(lnk, target_exe):
                        continue
                    if icon_file_for_shortcuts and icon_file_for_shortcuts.is_file():
                        update_shortcut_icon(lnk, f"{str(icon_file_for_shortcuts.resolve())},0")
                    os.utime(lnk, None)
                    print(f"[+] 已同步更新快捷方式: {lnk.name} -> {lnk.parent.name}")
                except Exception:
                    pass

    try:
        subprocess.run(
            ["taskkill", "/F", "/IM", "StartMenuExperienceHost.exe"],
            check=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        print("[+] 已重置 Windows 11 开始菜单宿主 (StartMenuExperienceHost)")
    except Exception:
        pass

    if restart_explorer:
        print("[*] 正在执行 Explorer 重启与图标缓存深度清理...")
        subprocess.run(["powershell", "-NoProfile", "-Command", "Stop-Process -Name explorer -Force"], check=False)
        time.sleep(1.0)
        explorer_dir = Path(os.path.expandvars(r"%LOCALAPPDATA%\Microsoft\Windows\Explorer"))
        deleted_count = 0
        for pattern in ["iconcache*.db", "thumbcache*.db"]:
            for f in explorer_dir.glob(pattern):
                try:
                    f.unlink()
                    deleted_count += 1
                except Exception:
                    pass
        iconcache_root = Path(os.path.expandvars(r"%LOCALAPPDATA%\IconCache.db"))
        if iconcache_root.is_file():
            try:
                iconcache_root.unlink()
                deleted_count += 1
            except Exception:
                pass
        print(f"[+] 已离线清除 {deleted_count} 个 Windows 图标缓存数据库")
        subprocess.run(["powershell", "-NoProfile", "-Command", "Start-Process explorer.exe"], check=False)
        time.sleep(1.2)

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
        description="Antigravity 任务栏/托盘/可执行文件图标修改与更新恢复工具",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--style", type=str, help="款式名称（如 underlit_chrome, dark_rainbow 等）")
    parser.add_argument("--custom-icon", type=str, help="自定义图标路径 (.ico)")
    parser.add_argument("--restore", "--revert", action="store_true", help="从 .bak 备份恢复官方原始 EXE 与快捷方式")
    parser.add_argument("--kill", action="store_true", help="强制关闭运行中的 Antigravity 进程")
    parser.add_argument("--restart-explorer", action="store_true", help="修改完成后自动离线重建图标缓存并重启 Explorer")
    parser.add_argument("--target-exe", type=str, help="手动指定 Antigravity.exe 路径")
    parser.add_argument("--rcedit-path", type=str, help="手动指定 rcedit.exe 路径")

    args = parser.parse_args()

    exe_path = find_target_exe(args.target_exe)
    print(f"[*] 目标可执行文件: {exe_path}")

    bak_path = exe_path.with_name(f"{exe_path.name}.bak")

    # 1. 还原逻辑
    if args.restore:
        if args.kill:
            subprocess.run(["taskkill", "/F", "/IM", exe_path.name], check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            time.sleep(0.5)

        if not bak_path.is_file():
            print("[-] 错误: 未找到官方备份文件 Antigravity.exe.bak，无法还原！", file=sys.stderr)
            return 1

        if is_file_locked(exe_path):
            old_path = exe_path.with_name(f"{exe_path.name}.old_running")
            try:
                if old_path.is_file():
                    old_path.unlink()
            except Exception:
                old_path = exe_path.with_name(f"{exe_path.name}.old_{int(time.time())}")
            exe_path.rename(old_path)
            shutil.copy2(bak_path, exe_path)
            print(f"[+] 已原子热替换还原官方原始 EXE: {exe_path}")
        else:
            shutil.copy2(bak_path, exe_path)
            print(f"[+] 已恢复官方原始 EXE: {exe_path}")

        refresh_windows_icon_cache(target_exe=exe_path, icon_file_for_shortcuts=exe_path, restart_explorer=args.restart_explorer)
        print("[SUCCESS] Antigravity 已成功还原为官方图标！")
        return 0

    # 2. 确定目标定制图标
    if args.custom_icon:
        target_ico = Path(args.custom_icon).resolve()
    elif args.style:
        target_ico = SCRIPT_DIR / "custom_icons" / f"{args.style}.ico"
    else:
        target_ico = SCRIPT_DIR / "custom_icons" / "underlit_chrome.ico"

    if not target_ico.is_file():
        print(f"[-] 错误: 指定的定制图标不存在: {target_ico}", file=sys.stderr)
        return 1

    print(f"[*] 使用定制图标: {target_ico.name} ({target_ico})")

    # 3. 准备备份
    if not bak_path.is_file():
        shutil.copy2(exe_path, bak_path)
        print(f"[+] 已创建官方原始 EXE 安全备份: {bak_path}")
    else:
        print(f"[*] 检测到已存在 EXE 原始备份: {bak_path} (保留原备份)")

    rcedit_exe = ensure_rcedit(args.rcedit_path)

    # 4. 执行注入：支持零感原子热替换
    if args.kill:
        print(f"[*] 执行 --kill，正在关闭 {exe_path.name}...")
        subprocess.run(["taskkill", "/F", "/IM", exe_path.name], check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        time.sleep(1.0)

    if not is_file_locked(exe_path):
        # 未被占用，直接注入
        print(f"[*] 正在调用 rcedit 直接写入 PE 图标: {exe_path.name}")
        if not apply_icon_to_file(rcedit_exe, exe_path, target_ico):
            return 1
    else:
        # 正在运行中：执行零感原子热替换
        print(f"[*] 检测到 {exe_path.name} 运行中，启动同分区零感原子热替换 (Zero-kill Hot Swap)...")
        staging_exe = exe_path.with_name(f"{exe_path.name}.new")
        shutil.copy2(exe_path, staging_exe)

        print(f"[*] 正在对临时载体写入 PE 图标: {staging_exe.name}")
        if not apply_icon_to_file(rcedit_exe, staging_exe, target_ico):
            try:
                staging_exe.unlink()
            except Exception:
                pass
            return 1

        old_running = exe_path.with_name(f"{exe_path.name}.old_running")
        try:
            if old_running.is_file():
                old_running.unlink()
        except Exception:
            old_running = exe_path.with_name(f"{exe_path.name}.old_{int(time.time())}")

        exe_path.rename(old_running)
        staging_exe.rename(exe_path)
        print(f"[+] 原子热替换完成！新版可执行文件已就位，原有进程不受任何中断影响。")

    # 5. 穿透刷新任务栏与系统图标
    refresh_windows_icon_cache(target_exe=exe_path, icon_file_for_shortcuts=target_ico, restart_explorer=args.restart_explorer)

    print("\n" + "=" * 60)
    print("[SUCCESS] Antigravity 任务栏与外壳图标补丁完成！")
    print(f"• 目标文件: {exe_path}")
    print(f"• 原始备份: {bak_path}")
    print(f"• 当前图标: {target_ico.name}")
    print("=" * 60)
    return 0


if __name__ == "__main__":
    sys.exit(main())
