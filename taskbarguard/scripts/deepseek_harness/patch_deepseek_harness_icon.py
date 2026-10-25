# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
"""
========================================================================================
DeepSeek Harness 任务栏/托盘/桌面端及 Chrome PWA 快捷方式图标精细化修改与自动更新恢复脚本 (优化加固版)
========================================================================================
"""

import argparse
import ctypes
import io
import json
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

SCRIPT_DIR = Path(__file__).resolve().parent

DEFAULT_EXE_CANDIDATES = [
    Path(r"D:\Users\29580\AppData\Local\Programs\DeepSeek Harness\DeepSeek Harness.exe"),
    Path(os.path.expandvars(r"%LOCALAPPDATA%\Programs\DeepSeek Harness\DeepSeek Harness.exe")),
]

RCEDIT_DEFAULT_PATH = Path(r"D:\Users\language_projects_bin\rcedit.exe")
RCEDIT_DOWNLOAD_URL = "https://github.com/electron/rcedit/releases/download/v2.0.0/rcedit-x64.exe"
ICO_SIZES = [16, 24, 32, 48, 64, 128, 256]

STATE_FILE = Path(os.path.expandvars(r"%APPDATA%\language_projects\taskbarguard\state.json"))


def load_state() -> dict:
    if STATE_FILE.is_file():
        try:
            with open(STATE_FILE, "r", encoding="utf-8") as f:
                return json.load(f)
        except Exception:
            return {}
    return {}


def save_state(state: dict) -> None:
    try:
        STATE_FILE.parent.mkdir(parents=True, exist_ok=True)
        with open(STATE_FILE, "w", encoding="utf-8") as f:
            json.dump(state, f, ensure_ascii=False, indent=2)
    except Exception as e:
        print(f"[-] 保存 state.json 失败: {e}", file=sys.stderr)


def find_target_exe(custom_path: str | None) -> Path | None:
    if custom_path:
        p = Path(custom_path).resolve()
        if not p.is_file():
            raise FileNotFoundError(f"未找到指定的 EXE 文件: {p}")
        return p

    for cand in DEFAULT_EXE_CANDIDATES:
        if cand.is_file():
            return cand

    return None


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


def detect_chrome_pwa_shortcuts() -> list[tuple[Path, str]]:
    """探测 Chrome PWA / Chrome 应用版的 DeepSeek Harness 快捷方式及其当前 IconLocation。

    采用 Python 原生目录扫描 + PowerShell 单个查询，彻底规避控制台编码冲突。
    返回列表 [(lnk_path, icon_location), ...]
    """
    candidate_dirs = [
        Path(os.path.expandvars(r"%APPDATA%\Microsoft\Windows\Start Menu\Programs\Chrome 应用")),
        Path(os.path.expandvars(r"%APPDATA%\Microsoft\Windows\Start Menu\Programs\Chrome Apps")),
        Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar")),
        Path(os.path.expandvars(r"%USERPROFILE%\Desktop")),
    ]

    candidate_lnks: list[Path] = []
    for d in candidate_dirs:
        if d.is_dir():
            for pat in ["*DeepSeek*.lnk", "*Harness*.lnk"]:
                for lnk in d.glob(pat):
                    if lnk.is_file() and lnk not in candidate_lnks:
                        candidate_lnks.append(lnk)

    results: list[tuple[Path, str]] = []
    for lnk in candidate_lnks:
        ps_cmd = f"""[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; $wsh = New-Object -ComObject WScript.Shell; $sc = $wsh.CreateShortcut('{str(lnk.resolve())}'); Write-Output "$($sc.TargetPath)<SEP>$($sc.Arguments)<SEP>$($sc.IconLocation)" """
        try:
            res = subprocess.run(
                ["powershell", "-NoProfile", "-Command", ps_cmd],
                capture_output=True,
                encoding="utf-8",
                errors="replace",
                creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
            )
            if res.returncode == 0 and res.stdout:
                parts = res.stdout.strip().split("<SEP>")
                if len(parts) >= 3:
                    target_path, args_str, icon_loc = parts[0].strip(), parts[1].strip(), parts[2].strip()
                    if "chrome_proxy.exe" in target_path.lower() or "--app-id=" in args_str or "harness" in lnk.name.lower():
                        results.append((lnk, icon_loc))
        except Exception as e:
            print(f"[-] 读取快捷方式属性失败 ({lnk.name}): {e}")

    return results


def update_shortcut_icon_location(lnk_path: Path, icon_path: Path) -> bool:
    """使用 WScript.Shell 将快捷方式的 IconLocation 指向新 ICO 文件。"""
    target_loc = f"{str(icon_path.resolve())}, 0"
    ps_cmd = f"""[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; $wsh = New-Object -ComObject WScript.Shell; $sc = $wsh.CreateShortcut('{str(lnk_path.resolve())}'); $sc.IconLocation = '{target_loc}'; $sc.Save()"""
    try:
        res = subprocess.run(
            ["powershell", "-NoProfile", "-Command", ps_cmd],
            capture_output=True,
            encoding="utf-8",
            errors="replace",
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        return res.returncode == 0
    except Exception:
        return False


def restore_shortcut_icon_location(lnk_path: Path, pristine_loc: str) -> bool:
    """使用 WScript.Shell 将快捷方式的 IconLocation 恢复为原生初态。"""
    ps_cmd = f"""[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; $wsh = New-Object -ComObject WScript.Shell; $sc = $wsh.CreateShortcut('{str(lnk_path.resolve())}'); $sc.IconLocation = '{pristine_loc}'; $sc.Save()"""
    try:
        res = subprocess.run(
            ["powershell", "-NoProfile", "-Command", ps_cmd],
            capture_output=True,
            encoding="utf-8",
            errors="replace",
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        return res.returncode == 0
    except Exception:
        return False


def patch_chrome_pwa_shortcuts(target_ico: Path) -> int:
    """自动扫描并对齐 Chrome PWA 版快捷方式的图标，并锁定原始初态。"""
    shortcuts = detect_chrome_pwa_shortcuts()
    if not shortcuts:
        return 0

    state = load_state()
    pwa_state = state.setdefault("chrome_pwa_harness_shortcuts", {})
    patched_count = 0

    for lnk_path, cur_icon_loc in shortcuts:
        lnk_key = str(lnk_path.resolve())
        if lnk_key not in pwa_state or "pristine_icon_location" not in pwa_state[lnk_key]:
            pwa_state[lnk_key] = {
                "pristine_icon_location": cur_icon_loc,
                "current_icon": str(target_ico.resolve()),
            }
            print(f"[+] 已锁定 Chrome 快捷方式原生初态: {lnk_path.name} -> {cur_icon_loc}")
        else:
            pwa_state[lnk_key]["current_icon"] = str(target_ico.resolve())

        if update_shortcut_icon_location(lnk_path, target_ico):
            print(f"[+] 已更新 Chrome 快捷方式图标: {lnk_path}")
            patched_count += 1
        else:
            print(f"[-] 更新快捷方式图标失败: {lnk_path}", file=sys.stderr)

    save_state(state)
    return patched_count


def restore_chrome_pwa_shortcuts() -> int:
    """根据 state.json 中记录的原生初态恢复 Chrome PWA 快捷方式。"""
    state = load_state()
    pwa_state = state.get("chrome_pwa_harness_shortcuts", {})
    if not pwa_state:
        return 0

    restored_count = 0
    for lnk_str, info in list(pwa_state.items()):
        lnk_path = Path(lnk_str)
        pristine_loc = info.get("pristine_icon_location")
        if not pristine_loc or not lnk_path.is_file():
            continue

        if restore_shortcut_icon_location(lnk_path, pristine_loc):
            print(f"[+] 已恢复 Chrome 快捷方式原生图标: {lnk_path.name} -> {pristine_loc}")
            restored_count += 1
            del pwa_state[lnk_str]
        else:
            print(f"[-] 恢复快捷方式图标失败: {lnk_path}", file=sys.stderr)

    save_state(state)
    return restored_count


def refresh_windows_icon_cache(restart_explorer: bool = False) -> None:
    print("[*] 正在安全刷新 Windows 任务栏与外壳图标缓存...")
    dirs_to_check = [
        Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar")),
        Path(os.path.expandvars(r"%APPDATA%\Microsoft\Windows\Start Menu\Programs")),
        Path(os.path.expandvars(r"%PROGRAMDATA%\Microsoft\Windows\Start Menu\Programs")),
        Path(os.path.expandvars(r"%USERPROFILE%\Desktop")),
        Path(os.path.expandvars(r"%PUBLIC%\Desktop")),
    ]
    for d in dirs_to_check:
        if d.is_dir():
            for pat in ["*DeepSeek*.lnk", "*Harness*.lnk"]:
                for lnk in d.rglob(pat):
                    try:
                        os.utime(lnk, None)
                        print(f"[+] 已刷新快捷方式时间戳: {lnk.name}")
                    except Exception:
                        pass

    # 注：安全下线强杀 StartMenuExperienceHost.exe。
    # 强制杀死 UWP 宿主会破坏 start2.bin 写入事务并被系统判定为异常崩溃，
    # 导致 Windows 11 触发自愈合并，重新追加 OEM/官方默认固定图标。
    # 图标刷新仅需依赖 utime + ie4uinit + SHChangeNotify。

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
        description="DeepSeek Harness 任务栏/托盘/桌面端及 Chrome PWA 快捷方式图标修改与更新恢复工具 (优化加固版)",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--custom-icon", type=str, help="自定义图标路径 (.ico)")
    parser.add_argument("--restore", "--revert", action="store_true", help="从备份与 state.json 恢复官方原始 EXE、托盘及快捷方式图标")
    parser.add_argument("--kill", action="store_true", help="如果应用正在运行或占用，强制关闭")
    parser.add_argument("--restart-explorer", action="store_true", help="修改完成后自动离线重建图标缓存并重启 Explorer")
    parser.add_argument("--target-exe", type=str, help="手动指定 DeepSeek Harness.exe 路径")
    parser.add_argument("--rcedit-path", type=str, help="手动指定 rcedit.exe 路径")
    parser.add_argument("--no-tray", action="store_true", help="不修改右下角托盘图标")

    args = parser.parse_args()

    exe_path = find_target_exe(args.target_exe)

    if args.restore:
        restored_any = False

        # 1. 还原 Chrome PWA 快捷方式
        pwa_restored = restore_chrome_pwa_shortcuts()
        if pwa_restored > 0:
            restored_any = True

        # 2. 还原桌面端 EXE 及托盘图标
        if exe_path and exe_path.is_file():
            ensure_process_closed(exe_path, kill=args.kill)
            bak_path = exe_path.with_name(f"{exe_path.name}.bak")
            tray_ico_path = exe_path.parent / "resources" / "tray.ico"
            tray_bak_path = exe_path.parent / "resources" / "tray.ico.bak"

            if bak_path.is_file():
                shutil.copy2(bak_path, exe_path)
                print(f"[+] 已恢复官方原始 EXE: {exe_path}")
                restored_any = True
            if tray_bak_path.is_file():
                shutil.copy2(tray_bak_path, tray_ico_path)
                print(f"[+] 已恢复官方原始托盘图标: {tray_ico_path}")
                restored_any = True

        if restored_any:
            refresh_windows_icon_cache(restart_explorer=args.restart_explorer)
            print("\n[SUCCESS] DeepSeek Harness 相关图标已全面恢复官方原貌！")
            return 0
        else:
            print("[-] 提示: 未发现需要还原的备份或记录。", file=sys.stderr)
            return 1

    # 确定目标图标
    if args.custom_icon:
        target_ico = Path(args.custom_icon).resolve()
    else:
        target_ico = SCRIPT_DIR / "custom_icons" / "8_underlit_chrome.ico"
        if not target_ico.is_file():
            target_ico = SCRIPT_DIR / "custom_icons" / "3_dark_monochrome_centered.ico"

    if not target_ico.is_file():
        print(f"[-] 错误: 指定的图标不存在: {target_ico}", file=sys.stderr)
        return 1

    print(f"[*] 使用定制质感图标: {target_ico.name} ({target_ico})")

    actions_taken = 0

    # 1. 对齐更新 Chrome PWA 快捷方式
    print("[*] 正在检查并对齐 Chrome PWA 版快捷方式...")
    pwa_count = patch_chrome_pwa_shortcuts(target_ico)
    if pwa_count > 0:
        actions_taken += pwa_count

    # 2. 对齐更新桌面端 EXE (如有)
    if exe_path and exe_path.is_file():
        print(f"[*] 发现桌面端可执行文件: {exe_path}")
        ensure_process_closed(exe_path, kill=args.kill)
        rcedit_exe = ensure_rcedit(args.rcedit_path)

        bak_path = exe_path.with_name(f"{exe_path.name}.bak")
        tray_ico_path = exe_path.parent / "resources" / "tray.ico"
        tray_bak_path = exe_path.parent / "resources" / "tray.ico.bak"

        if not bak_path.is_file():
            shutil.copy2(exe_path, bak_path)
            print(f"[+] 已创建官方原始 EXE 安全备份: {bak_path}")
        else:
            print(f"[*] 检测到已存在 EXE 安全备份: {bak_path} (保留原备份)")

        if tray_ico_path.is_file() and not tray_bak_path.is_file():
            shutil.copy2(tray_ico_path, tray_bak_path)
            print(f"[+] 已创建官方原始托盘图标安全备份: {tray_bak_path}")

        print(f"[*] 正在调用 rcedit 注入 PE 图标资源: {exe_path.name}")
        if apply_icon_with_retry(rcedit_exe, exe_path, target_ico):
            actions_taken += 1
            if not args.no_tray and tray_ico_path.parent.is_dir():
                try:
                    shutil.copy2(target_ico, tray_ico_path)
                    print(f"[+] 已同步更新右下角托盘图标: {tray_ico_path}")
                except Exception as e:
                    print(f"[-] 托盘图标写入异常: {e}")
        else:
            print(f"[-] 桌面端 PE 图标注入失败，保留快捷方式成果", file=sys.stderr)
    else:
        print("[*] 未检测到本地 Desktop Harness.exe，跳过 PE 资源注入")

    if actions_taken == 0:
        print("[-] 错误: 未找到可修补的 DeepSeek Harness EXE 或 Chrome 快捷方式！", file=sys.stderr)
        return 1

    refresh_windows_icon_cache(restart_explorer=args.restart_explorer)

    print("\n" + "=" * 60)
    print("[SUCCESS] DeepSeek Harness 任务栏、桌面端及 Chrome 快捷方式图标补丁完成！")
    if exe_path and exe_path.is_file():
        print(f"• 桌面端 EXE: {exe_path}")
    print(f"• 当前质感图标: {target_ico.name}")
    print(f"• 纳管 Chrome 快捷方式数: {pwa_count}")
    print("=" * 60)
    return 0


if __name__ == "__main__":
    sys.exit(main())
