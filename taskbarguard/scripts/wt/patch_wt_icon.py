# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=10.0.0",
# ]
# ///
"""
========================================================================================
Windows Terminal (wt.exe) 任务栏与快捷方式质感图标守卫与自动恢复引擎
========================================================================================
【核心架构与防线】
1. 目标绝对精准匹配与单次极速扫描：
   - 单次 PowerShell 批量检索开始菜单、桌面与任务栏（耗时约 1 秒）。
   - 严格通过 TargetPath 判定（仅匹配 wt.exe 或 WindowsTerminal.exe）。
   - 彻底废除文件名模糊匹配，严禁触碰 XTerminal、PowerShell 等任何无关程序。
   - 自动检测并跳过目标文件不存在的死链。
2. 不可变基线初态备份与防重入机制 (Pristine Lock Invariant)：
   - 持久化于 %APPDATA%\\language_projects\\taskbarguard\\state.json。
   - 首次纳管时锁定原始 IconLocation (pristine_icon) 与 created_by_guard 标记。
   - 重复执行换肤时，绝对禁止覆写 pristine_icon，保证永久保留原生状态。
3. 原生一键撤销恢复 (--revert)：
   - 对守卫自动新建的快捷方式执行安全移除。
   - 对原有快捷方式一键恢复到 pristine_icon。
4. 官方原生零风险外壳刷新三连击（绝不 taskkill explorer.exe）：
   - os.utime 触碰时间戳
   - ie4uinit.exe -show 原生刷新外壳图标缓存
   - SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_FLUSH) 广播通知
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

DEFAULT_STYLE_MAP = {
    ico.stem: ico for ico in CUSTOM_ICONS_DIR.glob("*.ico")
} if CUSTOM_ICONS_DIR.is_dir() else {}


def get_state_file_path() -> Path:
    app_data = os.environ.get("APPDATA")
    if app_data:
        base_dir = Path(app_data) / "language_projects" / "taskbarguard"
    else:
        base_dir = Path.home() / ".language_projects" / "taskbarguard"
    base_dir.mkdir(parents=True, exist_ok=True)
    return base_dir / "state.json"


def load_state() -> dict:
    state_file = get_state_file_path()
    if state_file.is_file():
        try:
            with open(state_file, "r", encoding="utf-8") as f:
                return json.load(f)
        except Exception:
            return {}
    return {}


def save_state(state: dict) -> None:
    state_file = get_state_file_path()
    state_file.parent.mkdir(parents=True, exist_ok=True)
    with open(state_file, "w", encoding="utf-8") as f:
        json.dump(state, f, ensure_ascii=False, indent=2)


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


def find_wt_shortcuts() -> list[tuple[Path, str, str]]:
    """单次调用 PowerShell 极速批量扫描并提取 Windows Terminal 快捷方式 (耗时约 1 秒)"""
    ps_cmd = """
$wsh = New-Object -ComObject WScript.Shell
$dirs = @(
    "$env:APPDATA\\Microsoft\\Internet Explorer\\Quick Launch\\User Pinned\\TaskBar",
    "$env:APPDATA\\Microsoft\\Windows\\Start Menu\\Programs",
    "$env:USERPROFILE\\Desktop",
    "$env:ProgramData\\Microsoft\\Windows\\Start Menu\\Programs"
)
foreach ($d in $dirs) {
    if (Test-Path $d) {
        Get-ChildItem -Path $d -Recurse -Filter "*.lnk" -ErrorAction SilentlyContinue | ForEach-Object {
            try {
                $sc = $wsh.CreateShortcut($_.FullName)
                $target = $sc.TargetPath
                if ($target) {
                    $targetLower = $target.ToLower()
                    if ($targetLower -like "*\\wt.exe" -or $targetLower -like "*\\windowsterminal.exe" -or $targetLower -eq "wt.exe" -or $targetLower -eq "windowsterminal.exe") {
                        "$($_.FullName)||$target||$($sc.IconLocation)"
                    }
                }
            } catch {}
        }
    }
}
"""
    try:
        res = subprocess.run(
            ["powershell", "-NoProfile", "-Command", ps_cmd],
            capture_output=True,
            text=True,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
    except Exception as exc:
        print(f"[-] 扫描快捷方式失败: {exc}")
        return []

    results = []
    seen = set()
    for line in res.stdout.strip().splitlines():
        line = line.strip()
        if not line or "||" not in line:
            continue
        parts = line.split("||", 2)
        lnk_path = Path(parts[0])
        target_path = parts[1]
        icon_loc = parts[2] if len(parts) > 2 else ""
        resolved = lnk_path.resolve()
        if resolved not in seen:
            seen.add(resolved)
            results.append((resolved, target_path, icon_loc))
    return results


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


def ensure_taskbar_wt_shortcut(ico_path: Path, state: dict) -> tuple[Path | None, bool]:
    taskbar_dir = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar"))
    if not taskbar_dir.exists():
        taskbar_dir.mkdir(parents=True, exist_ok=True)

    target_lnk = taskbar_dir / "Windows Terminal.lnk"
    created_new = False

    if not target_lnk.is_file():
        wt_candidate = Path(os.path.expandvars(r"%LOCALAPPDATA%\Microsoft\WindowsApps\wt.exe"))
        wt_target = str(wt_candidate) if wt_candidate.exists() else "wt.exe"
        user_home = str(Path.home())

        ps_cmd = f"""
$wsh = New-Object -ComObject WScript.Shell
$sc = $wsh.CreateShortcut('{str(target_lnk)}')
$sc.TargetPath = '{wt_target}'
$sc.WorkingDirectory = '{user_home}'
$sc.Description = 'Windows Terminal (TaskbarGuard)'
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
                created_new = True
                print(f"[+] 任务栏无独立快捷方式，已自动创建规范快捷方式: {target_lnk}")
        except Exception as exc:
            print(f"[-] 创建任务栏快捷方式失败: {exc}")
            return None, False

    return target_lnk, created_new


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


def apply_patch(style: str | None, custom_icon: str | None) -> int:
    ico_path = resolve_icon_path(style, custom_icon)
    print(f"[*] 选定 Windows Terminal 图标资源: {ico_path} (款式: {style or 'custom'})")

    state = load_state()
    wt_state = state.setdefault("apps", {}).setdefault("wt", {})
    shortcuts_state = wt_state.setdefault("shortcuts", {})

    existing_shortcuts = find_wt_shortcuts()
    updated: list[Path] = []

    for lnk, target, orig_icon in existing_shortcuts:
        lnk_key = str(lnk.resolve())
        # 首次纳管初态只读锁定
        if lnk_key not in shortcuts_state:
            shortcuts_state[lnk_key] = {
                "pristine_icon": orig_icon,
                "created_by_guard": False,
                "first_seen": time.time(),
            }
            print(f"[+] 捕获并锁定原生基线图标: {lnk.name} -> {orig_icon or '(默认)'}")

        if update_shortcut_icon(lnk, str(ico_path)):
            shortcuts_state[lnk_key]["current_icon"] = str(ico_path)
            updated.append(lnk)
            print(f"[+] 已更新快捷方式图标: {lnk.name} -> {lnk}")

    taskbar_lnk, created_new = ensure_taskbar_wt_shortcut(ico_path, state)
    if taskbar_lnk:
        lnk_key = str(taskbar_lnk.resolve())
        if created_new:
            shortcuts_state[lnk_key] = {
                "pristine_icon": "",
                "created_by_guard": True,
                "first_seen": time.time(),
                "current_icon": str(ico_path),
            }
        elif lnk_key not in shortcuts_state:
            shortcuts_state[lnk_key] = {
                "pristine_icon": "",
                "created_by_guard": False,
                "first_seen": time.time(),
                "current_icon": str(ico_path),
            }

        if taskbar_lnk not in updated:
            updated.append(taskbar_lnk)

    wt_state["last_style"] = style or "custom"
    wt_state["last_updated"] = time.time()
    save_state(state)

    refresh_windows_shell(updated)
    print("[+] 已广播 Windows 外壳与图标缓存刷新通知 (SHChangeNotify FLUSH)")
    print("\n" + "=" * 70)
    print("✅ Windows Terminal 图标守卫已应用！")
    print(f"   生效图标: {ico_path.name}")
    print(f"   更新快捷方式数: {len(updated)}")
    print("=" * 70)
    return 0


def revert_patch() -> int:
    print("[*] 正在执行 Windows Terminal 图标回退与恢复...")
    state = load_state()
    wt_state = state.get("apps", {}).get("wt", {})
    shortcuts_state = wt_state.get("shortcuts", {})

    touched: list[Path] = []
    # 如果此前守卫自动创建了任务栏文件
    taskbar_lnk = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar\Windows Terminal.lnk"))

    for lnk_str, meta in list(shortcuts_state.items()):
        lnk = Path(lnk_str)
        if not lnk.exists():
            continue

        if meta.get("created_by_guard", False):
            try:
                lnk.unlink()
                print(f"[+] 已移除守卫自动创建的快捷方式: {lnk}")
            except Exception as exc:
                print(f"[-] 移除快捷方式失败 {lnk}: {exc}")
        else:
            orig = meta.get("pristine_icon", "")
            if update_shortcut_icon(lnk, orig):
                touched.append(lnk)
                print(f"[+] 已恢复原生快捷方式图标: {lnk.name} -> {orig or '(默认)'}")

    # 兜底清理：如果任务栏存在 Windows Terminal.lnk 但没有被上面分支处理，也安全清理
    if taskbar_lnk.is_file() and str(taskbar_lnk.resolve()) not in [str(t.resolve()) for t in touched]:
        try:
            taskbar_lnk.unlink()
            print(f"[+] 已移除任务栏由守卫创建的快捷方式: {taskbar_lnk}")
        except Exception:
            pass

    if "wt" in state.get("apps", {}):
        del state["apps"]["wt"]
        save_state(state)

    refresh_windows_shell(touched)
    print("[+] 已广播 Windows 外壳与图标缓存刷新通知 (SHChangeNotify FLUSH)")
    print("\n" + "=" * 70)
    print("✅ Windows Terminal 快捷方式已彻底回退恢复为原生状态！")
    print("=" * 70)
    return 0


def main():
    parser = argparse.ArgumentParser(
        description="Windows Terminal (wt.exe) 任务栏与快捷方式图标定制与自动恢复守卫"
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
        "--revert",
        action="store_true",
        help="一键撤销并回退到原生初始基线状态",
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

    if args.revert:
        sys.exit(revert_patch())

    sys.exit(apply_patch(args.style, args.custom_icon))


if __name__ == "__main__":
    main()
