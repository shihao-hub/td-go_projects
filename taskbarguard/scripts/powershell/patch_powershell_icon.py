# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
"""
========================================================================================
PowerShell / Windows Terminal 任务栏与快捷方式图标定制与自动恢复守卫引擎
========================================================================================
【功能特性】
1. 智能定位 Windows Terminal (wt.exe) 与 PowerShell 相关的快捷方式与可执行文件：
   - 任务栏固定目录 (%APPDATA%\\Microsoft\\Internet Explorer\\Quick Launch\\User Pinned\\TaskBar)
   - 开始菜单程序目录 (%APPDATA%\\Microsoft\\Windows\\Start Menu\\Programs)
   - 用户桌面快捷方式 (%USERPROFILE%\\Desktop)
2. 自动化快捷方式创建与属性自愈：
   - 若任务栏尚未存在 Windows Terminal 快捷方式，自动创建规范快捷方式并绑定质感图标。
   - 自动更新已存在快捷方式的 IconLocation 属性。
3. 绿色独立版 PE 图标注入支持：
   - 若检测到独立免安装版 WindowsTerminal.exe 或 PowerShell 7 (pwsh.exe)，支持 rcedit 注入。
4. 官方原生零风险外壳刷新三连击（绝不 taskkill explorer.exe）：
   - os.utime 触碰快捷方式时间戳
   - ie4uinit.exe -show 原生刷新图标缓存
   - SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_FLUSH) 广播外壳变更通知
========================================================================================
"""

import argparse
import ctypes
import glob
import io
import os
import shutil
import subprocess
import sys
import time
import urllib.request
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
RCEDIT_DOWNLOAD_URL = "https://github.com/electron/rcedit/releases/download/v2.0.0/rcedit-x64.exe"

DEFAULT_STYLE_MAP = {
    ico.stem: ico for ico in CUSTOM_ICONS_DIR.glob("*.ico")
} if CUSTOM_ICONS_DIR.is_dir() else {}


def ensure_rcedit(custom_path: str | None = None) -> Path | None:
    """按需查找或下载 rcedit 工具 (仅在需要对 PE exe 注入时使用)"""
    if custom_path:
        p = Path(custom_path).resolve()
        if p.is_file():
            return p
        return None

    if RCEDIT_DEFAULT_PATH.is_file():
        return RCEDIT_DEFAULT_PATH

    path_which = shutil.which("rcedit") or shutil.which("rcedit-x64")
    if path_which:
        return Path(path_which)

    try:
        dest = RCEDIT_DEFAULT_PATH
        dest.parent.mkdir(parents=True, exist_ok=True)
        print(f"[*] 未检测到 rcedit 工具，正在自动从 GitHub 下载: {RCEDIT_DOWNLOAD_URL}")
        urllib.request.urlretrieve(RCEDIT_DOWNLOAD_URL, dest)
        print(f"[+] 下载完成: {dest}")
        return dest
    except Exception:
        return None


def resolve_icon_path(style: str | None, custom_icon: str | None) -> Path:
    """解析最终生效的 .ico 文件绝对路径"""
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


def find_target_shortcuts() -> list[Path]:
    """定位系统中所有与 Terminal / PowerShell 相关的快捷方式"""
    found: list[Path] = []
    seen_paths = set()

    search_dirs = [
        Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar")),
        Path(os.path.expandvars(r"%APPDATA%\Microsoft\Windows\Start Menu\Programs")),
        Path(os.path.expandvars(r"%USERPROFILE%\Desktop")),
        Path(os.path.expandvars(r"%ProgramData%\Microsoft\Windows\Start Menu\Programs")),
    ]

    keywords = ["terminal", "powershell", "wt", "终端"]

    for sdir in search_dirs:
        if not sdir.is_dir():
            continue
        try:
            for lnk in sdir.rglob("*.lnk"):
                name_lower = lnk.stem.lower()
                if any(kw in name_lower for kw in keywords):
                    resolved = lnk.resolve()
                    if resolved not in seen_paths:
                        seen_paths.add(resolved)
                        found.append(resolved)
        except Exception:
            continue

    return found


def update_shortcut_icon(lnk_path: Path, ico_path: Path) -> bool:
    """使用 PowerShell WScript.Shell 安全更新快捷方式的图标属性"""
    ps_cmd = f"""
$wsh = New-Object -ComObject WScript.Shell
$sc = $wsh.CreateShortcut('{str(lnk_path)}')
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
        return res.returncode == 0
    except Exception as exc:
        print(f"[-] 更新快捷方式失败 {lnk_path}: {exc}")
        return False


def ensure_taskbar_wt_shortcut(ico_path: Path) -> Path | None:
    """如果任务栏固定目录下没有 Windows Terminal 快捷方式，自动创建一个标准的快捷方式"""
    taskbar_dir = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar"))
    if not taskbar_dir.exists():
        taskbar_dir.mkdir(parents=True, exist_ok=True)

    target_lnk = taskbar_dir / "Windows Terminal.lnk"

    # 确定 wt.exe 路径
    wt_candidate = Path(os.path.expandvars(r"%LOCALAPPDATA%\Microsoft\WindowsApps\wt.exe"))
    wt_target = str(wt_candidate) if wt_candidate.exists() else "wt.exe"
    user_home = str(Path.home())

    ps_cmd = f"""
$wsh = New-Object -ComObject WScript.Shell
$sc = $wsh.CreateShortcut('{str(target_lnk)}')
$sc.TargetPath = '{wt_target}'
$sc.WorkingDirectory = '{user_home}'
$sc.Description = 'Windows Terminal (TaskbarGuard Custom Icon)'
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
    except Exception as exc:
        print(f"[-] 创建任务栏快捷方式失败: {exc}")

    return None


def find_standalone_exes(custom_exe: str | None = None) -> list[Path]:
    """发现可进行 PE 资源注入的独立 EXE (如绿色版 Terminal 或 pwsh.exe)"""
    exes = []
    if custom_exe:
        p = Path(custom_exe).resolve()
        if p.is_file():
            exes.append(p)
        return exes

    # 常见 PowerShell 7 与独立终端路径
    candidates = [
        Path(r"C:\Program Files\PowerShell\7\pwsh.exe"),
        Path(os.path.expandvars(r"%LOCALAPPDATA%\Programs\PowerShell\7\pwsh.exe")),
        Path(r"D:\Tools\WindowsTerminal\WindowsTerminal.exe"),
        Path(r"C:\Tools\WindowsTerminal\WindowsTerminal.exe"),
    ]

    for cand in candidates:
        if cand.is_file():
            exes.append(cand)

    return exes


def patch_pe_exe(exe_path: Path, ico_path: Path, rcedit_bin: Path) -> bool:
    """使用 rcedit 安全注入 PE 资源"""
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
            time.sleep(0.5)
        except Exception:
            time.sleep(0.5)
    return False


def refresh_windows_shell(touched_files: list[Path]) -> None:
    """零风险外壳刷新三连击 (绝不 taskkill explorer.exe)"""
    # 1. 碰触快捷方式时间戳
    now = time.time()
    for f in touched_files:
        try:
            if f.exists():
                os.utime(f, (now, now))
        except Exception:
            pass

    # 2. ie4uinit.exe -show 刷新图标缓存
    try:
        subprocess.run(
            ["ie4uinit.exe", "-show"],
            check=False,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
    except Exception:
        pass

    # 3. 广播 SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_FLUSH)
    try:
        SHCNE_ASSOCCHANGED = 0x08000000
        SHCNF_FLUSH = 0x1000
        ctypes.windll.shell32.SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_FLUSH, None, None)
    except Exception as exc:
        print(f"[-] 广播外壳刷新失败: {exc}")


def main():
    parser = argparse.ArgumentParser(
        description="PowerShell / Windows Terminal 任务栏与快捷方式图标定制与恢复守卫"
    )
    parser.add_argument(
        "--style",
        type=str,
        default="underlit_chrome",
        help="图标预设款式 (如 underlit_chrome, dark_blue, titanium, cyan_matrix, pure_dark)",
    )
    parser.add_argument(
        "--custom-icon",
        type=str,
        default=None,
        help="自定义 .ico 文件绝对路径",
    )
    parser.add_argument(
        "--target-exe",
        type=str,
        default=None,
        help="可选指定的独立 EXE 路径",
    )
    parser.add_argument(
        "--list-styles",
        action="store_true",
        help="列出所有可用预设款式并退出",
    )

    args = parser.parse_args()

    if args.list_styles:
        styles = list(DEFAULT_STYLE_MAP.keys())
        print("可用款式列表:")
        for s in styles:
            print(f"  - {s}")
        return

    ico_path = resolve_icon_path(args.style, args.custom_icon)
    print(f"[*] 选定图标资源: {ico_path} (款式/来源: {args.style or args.custom_icon})")

    # 1. 查找并更新快捷方式
    shortcuts = find_target_shortcuts()
    updated_shortcuts: list[Path] = []

    for lnk in shortcuts:
        # 跳过 VS Code 相关的 powershell 工具脚本快捷方式以防干扰
        if "visual studio" in str(lnk).lower():
            continue
        if update_shortcut_icon(lnk, ico_path):
            updated_shortcuts.append(lnk)
            print(f"[+] 已更新快捷方式图标: {lnk.name} -> {lnk}")

    # 2. 确保任务栏固定目录有专用的 Windows Terminal 快捷方式
    taskbar_lnk = ensure_taskbar_wt_shortcut(ico_path)
    if taskbar_lnk and taskbar_lnk not in updated_shortcuts:
        updated_shortcuts.append(taskbar_lnk)
        print(f"[+] 已创建/同步任务栏快捷方式: {taskbar_lnk}")

    # 3. 如果有可注入的独立 EXE，尝试注入
    standalone_exes = find_standalone_exes(args.target_exe)
    if standalone_exes:
        rcedit_bin = ensure_rcedit()
        if rcedit_bin:
            for exe in standalone_exes:
                patch_pe_exe(exe, ico_path, rcedit_bin)

    # 4. 刷新系统外壳
    refresh_windows_shell(updated_shortcuts)
    print("[+] 已广播 Windows 外壳与图标缓存刷新通知 (SHChangeNotify FLUSH)")

    print("\n" + "=" * 70)
    print("✅ PowerShell / Windows Terminal 图标守卫与应用完成！")
    print(f"   生效图标: {ico_path.name}")
    print(f"   更新快捷方式数: {len(updated_shortcuts)}")
    print("💡 任务栏提示：")
    print("   若任务栏原先固定的是 Windows Store 原版应用，建议在任务栏右键原图标")
    print("   选择「从任务栏取消固定」，随后点击桌面或开始菜单中已换上质感图标的")
    print("   Windows Terminal 并选择「固定到任务栏」，即可享受长效自愈的统一质感！")
    print("=" * 70)


if __name__ == "__main__":
    main()
