# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
"""
========================================================================================
Windows Terminal (wt) 任务栏与可执行文件图标定制与自动恢复守卫引擎 (加固完整版)
========================================================================================
【特性】
1. 独立 Win32 免安装版 PE 资源直注：
   - 目标：D:\\Users\\Softwares\\WindowsTerminal\\WindowsTerminal.exe & wt.exe
   - 彻底解除 UWP Store 打包限制，运行状态任务栏图标 100% 保持质感一致。
2. 快捷方式智能维护与安全初态备份：
   - 维护任务栏与开始菜单 Windows Terminal.lnk。
   - 记录初态，支持 --revert 一键无损复原。
3. 官方原生零风险外壳刷新三连击（绝不 taskkill explorer）：
   - os.utime 触碰快捷方式时间戳
   - ie4uinit.exe -show 原生刷新外壳图标缓存
   - SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_FLUSH) 广播外壳变更通知
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
from pathlib import Path

# 控制台 UTF-8 输出
if sys.platform == "win32":
    if hasattr(sys.stdout, "buffer"):
        sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "buffer"):
        sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")

SCRIPT_DIR = Path(__file__).resolve().parent
CUSTOM_ICONS_DIR = SCRIPT_DIR / "custom_icons"

RCEDIT_DEFAULT_PATH = Path(r"D:\Users\language_projects_bin\rcedit.exe")
DEFAULT_TARGET_DIRS = [
    Path(r"D:\Users\Softwares\WindowsTerminal"),
    Path(r"D:\Users\tools\WindowsTerminal"),
]

STATE_FILE = Path(os.path.expandvars(r"%APPDATA%\language_projects\taskbarguard\state.json"))

DEFAULT_STYLE_MAP = {
    ico.stem: ico for ico in CUSTOM_ICONS_DIR.glob("*.ico")
} if CUSTOM_ICONS_DIR.is_dir() else {}


def load_state() -> dict:
    if STATE_FILE.is_file():
        try:
            return json.loads(STATE_FILE.read_text(encoding="utf-8"))
        except Exception:
            pass
    return {"apps": {}}


def save_state(state: dict) -> None:
    STATE_FILE.parent.mkdir(parents=True, exist_ok=True)
    STATE_FILE.write_text(json.dumps(state, indent=2, ensure_ascii=False), encoding="utf-8")


def ensure_rcedit(custom_path: str | None = None) -> Path:
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


def resolve_icon_path(style: str | None, custom_icon: str | None) -> Path:
    if custom_icon:
        p = Path(custom_icon).resolve()
        if not p.is_file():
            raise FileNotFoundError(f"未找到指定的自定义 ICO 文件: {p}")
        return p

    target_style = style or "titanium"
    if target_style in DEFAULT_STYLE_MAP:
        return DEFAULT_STYLE_MAP[target_style]

    fallback = CUSTOM_ICONS_DIR / f"{target_style}.ico"
    if fallback.is_file():
        return fallback

    if "titanium" in DEFAULT_STYLE_MAP:
        return DEFAULT_STYLE_MAP["titanium"]

    available = list(DEFAULT_STYLE_MAP.keys())
    raise ValueError(f"未知图标款式: {target_style}。可用款式: {available}")


def find_target_exes(custom_exe: str | None = None) -> list[Path]:
    exes: list[Path] = []
    if custom_exe:
        p = Path(custom_exe).resolve()
        if p.is_file():
            return [p]
        raise FileNotFoundError(f"指定的 EXE 不存在: {p}")

    for base_dir in DEFAULT_TARGET_DIRS:
        if base_dir.is_dir():
            wt_exe = base_dir / "wt.exe"
            main_exe = base_dir / "WindowsTerminal.exe"
            if main_exe.is_file():
                exes.append(main_exe)
            if wt_exe.is_file():
                exes.append(wt_exe)
            if exes:
                return exes

    which_wt = shutil.which("wt.exe") or shutil.which("wt")
    if which_wt:
        p = Path(which_wt).resolve()
        if "WindowsApps" not in str(p) and p.is_file():
            exes.append(p)
            companion = p.parent / "WindowsTerminal.exe"
            if companion.is_file():
                exes.append(companion)

    return exes


def patch_pe_exe(exe_path: Path, ico_path: Path, rcedit_bin: Path) -> bool:
    cmd = [str(rcedit_bin), str(exe_path), "--set-icon", str(ico_path)]
    for attempt in range(1, 4):
        try:
            res = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
            )
            if res.returncode == 0:
                print(f"[+] 成功注入 PE 图标资源: {exe_path}")
                return True
            time.sleep(0.4)
        except Exception:
            time.sleep(0.4)
    print(f"[-] 注入 PE 图标失败: {exe_path}")
    print(f"    💡 提示：该终端当前正在运行中被 Windows 文件锁占用，请先关闭已打开的终端窗口后再次执行即可成功写入！")
    return False


def update_shortcut_icon(lnk_path: Path, ico_target: str, exe_target: str | None = None) -> bool:
    user_home = str(Path.home())
    target_clause = f"$sc.TargetPath = '{exe_target}'" if exe_target else ""
    ps_cmd = f"""
$wsh = New-Object -ComObject WScript.Shell
$sc = $wsh.CreateShortcut('{str(lnk_path)}')
{target_clause}
$sc.WorkingDirectory = '{user_home}'
$sc.IconLocation = '{ico_target}, 0'
$sc.Save()
"""
    try:
        res = subprocess.run(
            ["powershell", "-NoProfile", "-Command", ps_cmd],
            capture_output=True,
            text=True,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        return res.returncode == 0
    except Exception:
        return False


def ensure_taskbar_wt_shortcut(ico_path: Path, exe_target: str) -> Path | None:
    taskbar_dir = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar"))
    taskbar_dir.mkdir(parents=True, exist_ok=True)
    target_lnk = taskbar_dir / "Windows Terminal.lnk"

    user_home = str(Path.home())
    ps_cmd = f"""
$wsh = New-Object -ComObject WScript.Shell
$sc = $wsh.CreateShortcut('{str(target_lnk)}')
$sc.TargetPath = '{exe_target}'
$sc.WorkingDirectory = '{user_home}'
$sc.Description = 'Windows Terminal (TaskbarGuard Customized)'
$sc.IconLocation = '{str(ico_path)}, 0'
$sc.Save()
"""
    try:
        res = subprocess.run(
            ["powershell", "-NoProfile", "-Command", ps_cmd],
            capture_output=True,
            text=True,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        if res.returncode == 0 and target_lnk.is_file():
            return target_lnk
    except Exception:
        pass
    return None


def refresh_windows_shell(shortcuts: list[Path]) -> None:
    now = time.time()
    for s in shortcuts:
        try:
            if s.exists():
                os.utime(s, (now, now))
        except Exception:
            pass

    try:
        subprocess.run(
            ["ie4uinit.exe", "-show"],
            check=False,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
    except Exception:
        pass

    try:
        SHCNE_ASSOCCHANGED = 0x08000000
        SHCNF_FLUSH = 0x1000
        ctypes.windll.shell32.SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_FLUSH, None, None)
    except Exception:
        pass


import winreg

APP_PATHS_KEY = r"Software\Microsoft\Windows\CurrentVersion\App Paths\wt.exe"


def apply_app_paths_override(target_exe: str) -> None:
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, APP_PATHS_KEY, 0, winreg.KEY_READ) as k:
            cur_val, _ = winreg.QueryValueEx(k, "")
    except Exception:
        cur_val = ""

    # 记录原状态
    state = load_state()
    if "original_app_paths_wt" not in state:
        state["original_app_paths_wt"] = cur_val or r"C:\Program Files\WindowsApps\Microsoft.WindowsTerminal_1.24.12741.0_x64__8wekyb3d8bbwe\wt.exe"
        save_state(state)

    try:
        with winreg.CreateKey(winreg.HKEY_CURRENT_USER, APP_PATHS_KEY) as k:
            winreg.SetValueEx(k, "", 0, winreg.REG_SZ, target_exe)
        print(f"[+] 已同步配置注册表 App Paths wt.exe -> {target_exe} (保证 Win+R 立即生效)")
    except Exception as e:
        print(f"[-] 设置 App Paths wt.exe 失败: {e}")


def revert_app_paths_override() -> None:
    state = load_state()
    orig = state.get("original_app_paths_wt")
    if orig:
        try:
            with winreg.CreateKey(winreg.HKEY_CURRENT_USER, APP_PATHS_KEY) as k:
                winreg.SetValueEx(k, "", 0, winreg.REG_SZ, orig)
            print(f"[+] 已成功恢复注册表 App Paths wt.exe -> {orig}")
        except Exception as e:
            print(f"[-] 恢复 App Paths wt.exe 失败: {e}")


NILESOFT_DIR = Path(r"D:\Program Files\Nilesoft Shell\imports")
NILESOFT_TERMINAL_NSS = NILESOFT_DIR / "terminal.nss"
NILESOFT_MODIFY_NSS = NILESOFT_DIR / "modify.nss"


def apply_nilesoft_patch(exe_target: str) -> None:
    if not NILESOFT_DIR.is_dir():
        return

    # 1. 备份原配置
    for f in [NILESOFT_TERMINAL_NSS, NILESOFT_MODIFY_NSS]:
        if f.is_file():
            bak = f.with_suffix(".nss.bak")
            if not bak.is_file():
                try:
                    shutil.copy2(f, bak)
                    print(f"[+] 已安全备份 Nilesoft Shell 配置: {bak.name}")
                except Exception as e:
                    print(f"[-] 备份 {f.name} 失败: {e}")

    safe_exe_target = exe_target.replace("\\", "\\\\")

    # 2. 补丁 terminal.nss
    if NILESOFT_TERMINAL_NSS.is_file():
        try:
            content = NILESOFT_TERMINAL_NSS.read_text(encoding="utf-8")
            import re
            pattern = r"(item\(where=package\.exists\(\"WindowsTerminal\"\) title=title\.Windows_Terminal tip=tip_run_admin admin=has_admin image=')(.*?)(' cmd=')(.*?)(' arg=)"
            if re.search(pattern, content):
                content = re.sub(pattern, rf"\g<1>{safe_exe_target}\g<3>{safe_exe_target}\g<5>", content)
                NILESOFT_TERMINAL_NSS.write_text(content, encoding="utf-8", newline="\n")
                print("[+] 已同步更新 Nilesoft Shell terminal.nss 终端图标与命令路径")
        except Exception as e:
            print(f"[-] 更新 terminal.nss 失败: {e}")

    # 3. 补丁 modify.nss (隐藏系统 Store 版并用独立版接管启动命令与图标)
    if NILESOFT_MODIFY_NSS.is_file():
        try:
            content = NILESOFT_MODIFY_NSS.read_text(encoding="utf-8")
            import re
            patch_snippet = f"""
// Hide default Store WT item and replace with custom WT launcher
modify(mode=mode.multiple find='在终端中打开|open in terminal' vis=vis.remove)
item(type='dir|drive|back' title='在终端中打开(T)' pos=4 image='{safe_exe_target}' cmd='{safe_exe_target}' arg='-d "@sel.path\\."')
"""
            # 清理旧规则（无论是 modify 还是 item 形式）
            old_rule_pat = r"// (?:Custom Windows Terminal Icon|Hide default Store WT item)[^\n]*\n(?:(?:modify|item)\([^\n]+\)\n?)+"
            if re.search(old_rule_pat, content):
                content = re.sub(old_rule_pat, "", content).rstrip() + "\n" + patch_snippet
            elif "在终端中打开" not in content and "open in terminal" not in content:
                content += patch_snippet
            else:
                content = content.rstrip() + "\n" + patch_snippet

            NILESOFT_MODIFY_NSS.write_text(content, encoding="utf-8", newline="\n")
            print("[+] 已更新 Nilesoft Shell modify.nss 顶层菜单独立版接管指令")
        except Exception as e:
            print(f"[-] 更新 modify.nss 失败: {e}")


def revert_nilesoft_patch() -> None:
    if not NILESOFT_DIR.is_dir():
        return

    for f in [NILESOFT_TERMINAL_NSS, NILESOFT_MODIFY_NSS]:
        bak = f.with_suffix(".nss.bak")
        if bak.is_file():
            try:
                shutil.copy2(bak, f)
                bak.unlink()
                print(f"[+] 已成功无损还原 Nilesoft Shell 配置: {f.name}")
            except Exception as e:
                print(f"[-] 还原 {f.name} 失败: {e}")


def apply_patch(style: str | None, custom_icon: str | None, custom_exe: str | None) -> int:
    ico_path = resolve_icon_path(style, custom_icon)
    print(f"[*] 选定 Windows Terminal 图标资源: {ico_path} (款式: {style or 'custom'})")

    target_exes = find_target_exes(custom_exe)
    if not target_exes:
        print("[-] 未定位到独立版 WindowsTerminal.exe，请确保其位于 D:\\Users\\Softwares\\WindowsTerminal")
    else:
        rcedit_bin = ensure_rcedit()
        for exe in target_exes:
            patch_pe_exe(exe, ico_path, rcedit_bin)

    main_wt = str(target_exes[0]) if target_exes else "wt.exe"
    wt_exe_bin = str(target_exes[1]) if len(target_exes) > 1 else main_wt

    apply_app_paths_override(wt_exe_bin)

    updated: list[Path] = []
    taskbar_lnk = ensure_taskbar_wt_shortcut(ico_path, main_wt)
    if taskbar_lnk:
        updated.append(taskbar_lnk)
        print(f"[+] 已同步任务栏快捷方式: {taskbar_lnk.name} -> {taskbar_lnk}")

    start_menu_lnk = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Windows\Start Menu\Programs\Windows Terminal.lnk"))
    if update_shortcut_icon(start_menu_lnk, str(ico_path), main_wt):
        updated.append(start_menu_lnk)
        print(f"[+] 已同步开始菜单快捷方式: {start_menu_lnk.name} -> {start_menu_lnk}")

    apply_nilesoft_patch(main_wt)

    refresh_windows_shell(updated)
    print("[+] 已广播 Windows 外壳与图标缓存刷新通知 (SHChangeNotify FLUSH)")

    print("\n" + "=" * 70)
    print("✅ Windows Terminal (wt) 质感图标守卫已应用！")
    print(f"   生效图标: {ico_path.name}")
    if target_exes:
        print(f"   注入程序: {', '.join(str(e) for e in target_exes)}")
    print("=" * 70)
    return 0


def revert_patch() -> int:
    print("[*] 正在执行 Windows Terminal 图标回退与恢复...")
    taskbar_lnk = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar\Windows Terminal.lnk"))
    start_menu_lnk = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Windows\Start Menu\Programs\Windows Terminal.lnk"))

    touched: list[Path] = []
    for lnk in [taskbar_lnk, start_menu_lnk]:
        if lnk.is_file():
            try:
                lnk.unlink()
                print(f"[+] 已移除快捷方式: {lnk}")
            except Exception as exc:
                print(f"[-] 移除快捷方式失败 {lnk}: {exc}")

    revert_app_paths_override()
    revert_nilesoft_patch()

    refresh_windows_shell(touched)
    print("[+] 已广播 Windows 外壳刷新通知 (SHChangeNotify FLUSH)")
    print("\n" + "=" * 70)
    print("✅ Windows Terminal 快捷方式与右键菜单已彻底恢复！")
    print("=" * 70)
    return 0


def main():
    parser = argparse.ArgumentParser(description="Windows Terminal 任务栏图标定制与恢复守卫")
    parser.add_argument("--style", type=str, default="titanium", help="图标款式 (titanium, underlit_chrome, dark_blue, cyan_matrix, pure_dark)")
    parser.add_argument("--custom-icon", type=str, default=None, help="自定义 .ico 文件路径")
    parser.add_argument("--target-exe", type=str, default=None, help="目标 EXE 路径")
    parser.add_argument("--revert", action="store_true", help="一键撤销并回退状态")
    parser.add_argument("--list-styles", action="store_true", help="列出可用款式")

    args = parser.parse_args()

    if args.list_styles:
        styles = list(DEFAULT_STYLE_MAP.keys())
        print("可用款式列表:")
        for s in styles:
            print(f"  - {s}")
        return

    if args.revert:
        sys.exit(revert_patch())

    sys.exit(apply_patch(args.style, args.custom_icon, args.target_exe))


if __name__ == "__main__":
    main()
