# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
"""
========================================================================================
PowerShell 7 (pwsh.exe) 任务栏与快捷方式质感图标守卫引擎 (精准安全版)
========================================================================================
【安全规范】
1. 精准目标过滤：
   - 快捷方式 TargetPath 必须严格以 \\pwsh.exe 结尾。
   - 严禁通过文件名模糊匹配，严禁触碰 Windows Terminal、XTerminal、PowerShell 5.1。
2. 初态只读锁定与基线备份：
   - 快捷方式初态记录于 %APPDATA%\\language_projects\\taskbarguard\\state.json。
   - EXE 注入前备份原版到 %APPDATA%\\language_projects\\taskbarguard\\backups\\pwsh.pristine.exe。
   - 重复执行绝不覆写初态备份。
3. 一键撤销恢复 (--revert)：
   - 快捷方式恢复 pristine_icon。
   - EXE 使用 pristine 备份或官方图标无损复原。
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

# 控制台 UTF-8 输出防乱码
if sys.platform == "win32":
    if hasattr(sys.stdout, "buffer"):
        sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "buffer"):
        sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")

SCRIPT_DIR = Path(__file__).resolve().parent
CUSTOM_ICONS_DIR = SCRIPT_DIR / "custom_icons"

RCEDIT_DEFAULT_PATH = Path(r"D:\Users\language_projects_bin\rcedit.exe")

DEFAULT_STYLE_MAP = {
    ico.stem: ico for ico in CUSTOM_ICONS_DIR.glob("*.ico")
} if CUSTOM_ICONS_DIR.is_dir() else {}


def get_base_dir() -> Path:
    app_data = os.environ.get("APPDATA")
    if app_data:
        base_dir = Path(app_data) / "language_projects" / "taskbarguard"
    else:
        base_dir = Path.home() / ".language_projects" / "taskbarguard"
    base_dir.mkdir(parents=True, exist_ok=True)
    return base_dir


def load_state() -> dict:
    state_file = get_base_dir() / "state.json"
    if state_file.is_file():
        try:
            with open(state_file, "r", encoding="utf-8") as f:
                return json.load(f)
        except Exception:
            return {}
    return {}


def save_state(state: dict) -> None:
    state_file = get_base_dir() / "state.json"
    with open(state_file, "w", encoding="utf-8") as f:
        json.dump(state, f, ensure_ascii=False, indent=2)


def ensure_rcedit() -> Path | None:
    if RCEDIT_DEFAULT_PATH.is_file():
        return RCEDIT_DEFAULT_PATH
    path_which = shutil.which("rcedit") or shutil.which("rcedit-x64")
    if path_which:
        return Path(path_which)
    return None


def resolve_icon_path(style: str | None, custom_icon: str | None) -> Path:
    if custom_icon:
        p = Path(custom_icon).resolve()
        if not p.is_file():
            raise FileNotFoundError(f"未找到指定的自定义 ICO 文件: {p}")
        return p

    target_style = style or "underlit_chrome"
    if target_style in DEFAULT_STYLE_MAP:
        return DEFAULT_STYLE_MAP[target_style]

    fallback = CUSTOM_ICONS_DIR / f"{target_style}.ico"
    if fallback.is_file():
        return fallback

    if "underlit_chrome" in DEFAULT_STYLE_MAP:
        return DEFAULT_STYLE_MAP["underlit_chrome"]

    available = list(DEFAULT_STYLE_MAP.keys())
    raise ValueError(f"未知图标款式: {target_style}。可用款式: {available}")


def inspect_shortcut(lnk_path: Path) -> tuple[str, str] | None:
    ps_cmd = f"""
$wsh = New-Object -ComObject WScript.Shell
$sc = $wsh.CreateShortcut('{str(lnk_path)}')
Write-Output "$($sc.TargetPath)||$($sc.IconLocation)"
"""
    try:
        res = subprocess.run(
            ["powershell", "-NoProfile", "-Command", ps_cmd],
            capture_output=True,
            text=True,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        if res.returncode == 0 and res.stdout.strip():
            parts = res.stdout.strip().split("||", 1)
            target = parts[0].strip() if len(parts) > 0 else ""
            icon = parts[1].strip() if len(parts) > 1 else ""
            return target, icon
    except Exception:
        pass
    return None


def is_pwsh_target(target_path: str) -> bool:
    if not target_path:
        return False
    lower = target_path.strip().lower()
    return lower == "pwsh.exe" or lower.endswith("\\pwsh.exe") or lower.endswith("/pwsh.exe")


def find_pwsh_shortcuts() -> list[Path]:
    """严格通过 TargetPath 仅匹配 PowerShell 7 (pwsh.exe) 快捷方式"""
    found: list[Path] = []
    seen = set()

    search_dirs = [
        Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar")),
        Path(os.path.expandvars(r"%APPDATA%\Microsoft\Windows\Start Menu\Programs")),
        Path(os.path.expandvars(r"%USERPROFILE%\Desktop")),
        Path(os.path.expandvars(r"%ProgramData%\Microsoft\Windows\Start Menu\Programs")),
    ]

    for sdir in search_dirs:
        if not sdir.is_dir():
            continue
        try:
            for lnk in sdir.rglob("*.lnk"):
                resolved = lnk.resolve()
                if resolved in seen:
                    continue
                info = inspect_shortcut(resolved)
                if not info:
                    continue
                target, _ = info
                if is_pwsh_target(target):
                    if ":" in target and not Path(target).exists():
                        print(f"[!] 跳过 PowerShell 7 死链: {resolved}")
                        continue
                    seen.add(resolved)
                    found.append(resolved)
        except Exception:
            continue

    return found


def update_shortcut_icon(lnk_path: Path, ico_path_or_spec: str) -> bool:
    val = ico_path_or_spec
    if not val.endswith(",0") and not val.endswith(", 0") and val.endswith(".ico"):
        val = f"{val}, 0"

    ps_cmd = f"""
$wsh = New-Object -ComObject WScript.Shell
$sc = $wsh.CreateShortcut('{str(lnk_path)}')
$sc.IconLocation = '{val}'
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
    except Exception as exc:
        print(f"[-] 更新快捷方式失败 {lnk_path}: {exc}")
        return False


def find_standalone_pwsh_exes(custom_exe: str | None = None) -> list[Path]:
    exes = []
    if custom_exe:
        p = Path(custom_exe).resolve()
        if p.is_file():
            exes.append(p)
        return exes

    candidates = [
        Path(r"C:\Program Files\PowerShell\7\pwsh.exe"),
        Path(os.path.expandvars(r"%LOCALAPPDATA%\Programs\PowerShell\7\pwsh.exe")),
    ]
    for cand in candidates:
        if cand.is_file():
            exes.append(cand)
    return exes


def backup_exe_if_needed(exe_path: Path) -> Path | None:
    backup_dir = get_base_dir() / "backups"
    backup_dir.mkdir(parents=True, exist_ok=True)
    backup_file = backup_dir / f"{exe_path.stem}.pristine.exe"

    if not backup_file.is_file():
        try:
            shutil.copy2(exe_path, backup_file)
            print(f"[+] 首次执行，已锁定并建立 PE 二进制纯净基线备份: {backup_file}")
            return backup_file
        except Exception as exc:
            print(f"[-] 备份 EXE 失败: {exc}")
            return None
    return backup_file


def patch_pe_exe(exe_path: Path, ico_path: Path, rcedit_bin: Path) -> bool:
    backup_exe_if_needed(exe_path)
    cmd = [str(rcedit_bin), str(exe_path), "--set-icon", str(ico_path)]
    for _ in range(3):
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
            time.sleep(0.5)
        except Exception:
            time.sleep(0.5)
    return False


def refresh_windows_shell(touched_files: list[Path]) -> None:
    now = time.time()
    for f in touched_files:
        try:
            if f.exists():
                os.utime(f, (now, now))
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
    except Exception as exc:
        print(f"[-] 广播外壳刷新失败: {exc}")


def apply_patch(style: str | None, custom_icon: str | None, target_exe: str | None) -> int:
    ico_path = resolve_icon_path(style, custom_icon)
    print(f"[*] 选定 PowerShell 7 图标资源: {ico_path} (款式: {style or 'custom'})")

    state = load_state()
    ps_state = state.setdefault("apps", {}).setdefault("powershell", {})
    shortcuts_state = ps_state.setdefault("shortcuts", {})

    shortcuts = find_pwsh_shortcuts()
    updated: list[Path] = []

    for lnk in shortcuts:
        lnk_key = str(lnk.resolve())
        if lnk_key not in shortcuts_state:
            info = inspect_shortcut(lnk)
            orig_icon = info[1] if info else ""
            shortcuts_state[lnk_key] = {
                "pristine_icon": orig_icon,
                "first_seen": time.time(),
            }
            print(f"[+] 锁定原生快捷方式基线图标: {lnk.name} -> {orig_icon or '(默认)'}")

        if update_shortcut_icon(lnk, str(ico_path)):
            shortcuts_state[lnk_key]["current_icon"] = str(ico_path)
            updated.append(lnk)
            print(f"[+] 已更新快捷方式图标: {lnk.name} -> {lnk}")

    standalone_exes = find_standalone_pwsh_exes(target_exe)
    if standalone_exes:
        rcedit_bin = ensure_rcedit()
        if rcedit_bin:
            for exe in standalone_exes:
                patch_pe_exe(exe, ico_path, rcedit_bin)

    save_state(state)
    refresh_windows_shell(updated)
    print("\n" + "=" * 70)
    print("✅ PowerShell 7 图标守卫已应用！")
    print(f"   生效图标: {ico_path.name}")
    print(f"   更新快捷方式数: {len(updated)}")
    print("=" * 70)
    return 0


def revert_patch() -> int:
    print("[*] 正在执行 PowerShell 7 图标回退与恢复...")
    state = load_state()
    ps_state = state.get("apps", {}).get("powershell", {})
    shortcuts_state = ps_state.get("shortcuts", {})

    touched: list[Path] = []
    for lnk_str, meta in list(shortcuts_state.items()):
        lnk = Path(lnk_str)
        if not lnk.exists():
            continue
        orig = meta.get("pristine_icon", "")
        if not orig:
            info = inspect_shortcut(lnk)
            target = info[0] if info else ""
            orig = f"{target}, 0" if target else ""
        if update_shortcut_icon(lnk, orig):
            touched.append(lnk)
            print(f"[+] 已恢复原生快捷方式图标: {lnk.name} -> {orig or '(默认)'}")

    # 恢复 EXE
    backup_file = get_base_dir() / "backups" / "pwsh.pristine.exe"
    rcedit_bin = ensure_rcedit()
    for exe in find_standalone_pwsh_exes():
        if backup_file.is_file():
            try:
                shutil.copy2(backup_file, exe)
                print(f"[+] 已从纯净基线文件还原 EXE: {exe}")
            except Exception as exc:
                print(f"[-] 还原 EXE 失败: {exc}")
        elif rcedit_bin:
            # 回退官方图标
            official_ico = get_base_dir() / "assets" / "Powershell_black.ico"
            if official_ico.is_file():
                patch_pe_exe(exe, official_ico, rcedit_bin)
                print(f"[+] 已注入微软官方图标恢复 EXE: {exe}")

    if "powershell" in state.get("apps", {}):
        del state["apps"]["powershell"]
        save_state(state)

    refresh_windows_shell(touched)
    print("\n" + "=" * 70)
    print("✅ PowerShell 7 已彻底回退恢复为原生状态！")
    print("=" * 70)
    return 0


def main():
    parser = argparse.ArgumentParser(
        description="PowerShell 7 (pwsh.exe) 任务栏与快捷方式图标定制与恢复守卫"
    )
    parser.add_argument("--style", type=str, default="underlit_chrome", help="图标预设款式")
    parser.add_argument("--custom-icon", type=str, default=None, help="自定义 .ico 文件绝对路径")
    parser.add_argument("--target-exe", type=str, default=None, help="可选指定的独立 EXE 路径")
    parser.add_argument("--revert", action="store_true", help="一键撤销并回退到原生初始基线状态")
    parser.add_argument("--list-styles", action="store_true", help="列出所有可用预设款式并退出")

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
