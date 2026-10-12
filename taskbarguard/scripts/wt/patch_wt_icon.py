# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
========================================================================================
Windows Terminal (wt) 任务栏与可执行文件图标定制与自动恢复守卫引擎 (加固增强版)
========================================================================================
【升级特性】
1. 纯 Win32 原生 PE 资源注入：
   - 彻底摆脱 rcedit.exe 等外部工具依赖；
   - 全量覆盖 Group 101 (默认)、Group 102 (高对比黑)、Group 103 (高对比白)；
   - 同步覆盖 Images/ 物理对比度图标，彻底消灭高对比与浅色主题下的原版反弹。
2. 运行中 EXE 原子热替换：
   - 突破 Windows 内核执行独占锁 (ERROR_ACCESS_DENIED)；
   - 采用同分区原子重命名 (.old_running) 热替换，无需强杀正在跑服务的常驻终端（亦保留 --kill 选项）。
3. 任务栏与开始菜单快捷方式安全规范：
   - 仅维护标准开始菜单快捷方式，杜绝强写 User Pinned TaskBar 导致的任务栏右键菜单丢失固定项死锁；
   - 自动检测并清理孤立冲突的任务栏快捷方式，恢复原生固定机制。
4. 全链路接管：
   - 接管 Win+R (App Paths) 阻断系统劫持回 Store 原版；
   - 接管 Nilesoft Shell 右键菜单；
   - 支持 --auto-setup 自动拉取官方最新免安装版独立终端并配置 PATH（仅目录为空时联网部署）。
5. 独立版幂等升级（--update）：
   - 比对本地 exe VersionInfo 与 GitHub 最新稳定版，已是最新则直接退出 0，可安全重复执行；
   - 暂存目录解压 + 完整性校验 + 目录级原子换入，失败自动回滚，旧版始终可用；
   - 升级后自动沿用 state.json 记录的图标款式重放全链路定制。
6. 深度离线缓存重构与 100% 官方原生无损复原：
   - 支持 --restart-explorer 离线清空全部 30 个 iconcache_*.db 彻底重构外壳图标缓存；
   - 支持 --revert 一键无损复原所有配置。
========================================================================================
"""

import argparse
import ctypes
from ctypes import wintypes
import io
import json
import os
import re
import shutil
import struct
import subprocess
import sys
import time
import urllib.request
import winreg
import zipfile
from pathlib import Path

# 控制台 UTF-8 输出
if sys.platform == "win32":
    if hasattr(sys.stdout, "buffer"):
        sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "buffer"):
        sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")

SCRIPT_DIR = Path(__file__).resolve().parent
CUSTOM_ICONS_DIR = SCRIPT_DIR / "custom_icons"

DEFAULT_TARGET_DIRS = [
    Path(r"D:\Users\Softwares\WindowsTerminal"),
    Path(r"D:\Users\tools\WindowsTerminal"),
]

GITHUB_LATEST_WT_API = "https://api.github.com/repos/microsoft/terminal/releases/latest"
USER_AGENT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
WT_ASSET_PREFIX = "Microsoft.WindowsTerminal_"
WT_ASSET_SUFFIX = "_x64.zip"
FALLBACK_WT_VERSION = "1.25.2733.0"
FALLBACK_WT_ASSET = f"Microsoft.WindowsTerminal_{FALLBACK_WT_VERSION}_x64.zip"
FALLBACK_WT_URL = (
    f"https://github.com/microsoft/terminal/releases/download/v{FALLBACK_WT_VERSION}/{FALLBACK_WT_ASSET}"
)
UPGRADE_STAGING_SUFFIX = ".new"
UPGRADE_BACKUP_SUFFIX = ".old"
UPGRADE_MAIN_EXE = "WindowsTerminal.exe"
UPGRADE_REQUIRED_FILES = ("WindowsTerminal.exe", "wt.exe")

STATE_FILE = Path(os.path.expandvars(r"%APPDATA%\language_projects\taskbarguard\state.json"))
APP_PATHS_KEY = r"Software\Microsoft\Windows\CurrentVersion\App Paths\wt.exe"
CONSOLE_STARTUP_KEY = r"Console\%%Startup"
GUID_CONHOST = "{B23D10C0-E52E-411E-9D5B-C09FDF709C7D}"
GUID_WT = "{2EACA947-7F5F-4CFA-BA87-8F7FBEEFBE69}"
GUID_AUTO = "{00000000-0000-0000-0000-000000000000}"

NILESOFT_CANDIDATE_DIRS = [
    Path(r"D:\Program Files\Nilesoft Shell\imports"),
    Path(r"C:\Program Files\Nilesoft Shell\imports"),
]

# Win32 API 声明
kernel32 = ctypes.windll.kernel32
user32 = ctypes.windll.user32
shell32 = ctypes.windll.shell32

RT_ICON = 3
RT_GROUP_ICON = 14

kernel32.BeginUpdateResourceW.argtypes = [wintypes.LPCWSTR, wintypes.BOOL]
kernel32.BeginUpdateResourceW.restype = wintypes.HANDLE

kernel32.UpdateResourceW.argtypes = [
    wintypes.HANDLE,
    ctypes.c_void_p,
    ctypes.c_void_p,
    wintypes.WORD,
    ctypes.c_void_p,
    wintypes.DWORD,
]
kernel32.UpdateResourceW.restype = wintypes.BOOL

kernel32.EndUpdateResourceW.argtypes = [wintypes.HANDLE, wintypes.BOOL]
kernel32.EndUpdateResourceW.restype = wintypes.BOOL


class VS_FIXEDFILEINFO(ctypes.Structure):
    """Win32 版本资源固定信息块（用于提取 dwFileVersionMS/LS 四段版本号）"""

    _fields_ = [
        ("dwSignature", wintypes.DWORD),
        ("dwStrucVersion", wintypes.DWORD),
        ("dwFileVersionMS", wintypes.DWORD),
        ("dwFileVersionLS", wintypes.DWORD),
        ("dwProductVersionMS", wintypes.DWORD),
        ("dwProductVersionLS", wintypes.DWORD),
        ("dwFileFlagsMask", wintypes.DWORD),
        ("dwFileFlags", wintypes.DWORD),
        ("dwFileOS", wintypes.DWORD),
        ("dwFileType", wintypes.DWORD),
        ("dwFileSubtype", wintypes.DWORD),
        ("dwFileDateMS", wintypes.DWORD),
        ("dwFileDateLS", wintypes.DWORD),
    ]


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


def get_available_styles() -> dict[str, Path]:
    if not CUSTOM_ICONS_DIR.is_dir():
        return {}
    return {ico.stem: ico for ico in CUSTOM_ICONS_DIR.glob("*.ico")}


def parse_ico(ico_bytes: bytes):
    idReserved, idType, idCount = struct.unpack("<HHH", ico_bytes[:6])
    if idReserved != 0 or idType != 1:
        raise ValueError("非法的 ICO 文件格式")

    entries = []
    images = []
    for i in range(idCount):
        offset = 6 + i * 16
        bWidth, bHeight, bColorCount, bReserved, wPlanes, wBitCount, dwBytesInRes, dwImageOffset = struct.unpack(
            "<BBBBHHII", ico_bytes[offset : offset + 16]
        )
        img_data = ico_bytes[dwImageOffset : dwImageOffset + dwBytesInRes]
        entries.append({
            "bWidth": bWidth,
            "bHeight": bHeight,
            "bColorCount": bColorCount,
            "bReserved": bReserved,
            "wPlanes": wPlanes,
            "wBitCount": wBitCount,
            "dwBytesInRes": dwBytesInRes,
        })
        images.append(img_data)
    return entries, images


def update_group_icon_in_file(exe_path: Path, group_id: int, ico_path: Path, start_icon_id: int = 1) -> bool:
    ico_bytes = ico_path.read_bytes()
    entries, images = parse_ico(ico_bytes)

    hUpdate = kernel32.BeginUpdateResourceW(str(exe_path), False)
    if not hUpdate:
        raise OSError(f"BeginUpdateResourceW 失败: {kernel32.GetLastError()}")

    try:
        grp_header = struct.pack("<HHH", 0, 1, len(entries))
        grp_entries = []
        for i, ent in enumerate(entries):
            icon_id = start_icon_id + i
            grp_entry = struct.pack(
                "<BBBBHHIH",
                ent["bWidth"],
                ent["bHeight"],
                ent["bColorCount"],
                ent["bReserved"],
                ent["wPlanes"],
                ent["wBitCount"],
                ent["dwBytesInRes"],
                icon_id,
            )
            grp_entries.append(grp_entry)

            img_data = images[i]
            res = kernel32.UpdateResourceW(
                hUpdate, RT_ICON, icon_id, 0, img_data, len(img_data)
            )
            if not res:
                raise OSError(f"UpdateResource RT_ICON 失败: {kernel32.GetLastError()}")

        grp_data = grp_header + b"".join(grp_entries)
        res_grp = kernel32.UpdateResourceW(
            hUpdate, RT_GROUP_ICON, group_id, 0, grp_data, len(grp_data)
        )
        if not res_grp:
            raise OSError(f"UpdateResource RT_GROUP_ICON 失败: {kernel32.GetLastError()}")

        if not kernel32.EndUpdateResourceW(hUpdate, False):
            raise OSError(f"EndUpdateResourceW 失败: {kernel32.GetLastError()}")
        return True
    except Exception:
        kernel32.EndUpdateResourceW(hUpdate, True)
        raise


def safe_patch_pe_binary(target_exe: Path, ico_path: Path, group_ids: list[int]) -> bool:
    print(f"[*] 正在为 {target_exe.name} 注入定制图标 (Group IDs: {group_ids})...")
    temp_patched = target_exe.with_suffix(".exe.new_patch")
    shutil.copy2(target_exe, temp_patched)

    start_id = 1
    for gid in group_ids:
        update_group_icon_in_file(temp_patched, gid, ico_path, start_icon_id=start_id)
        start_id += 20

    old_backup = target_exe.with_suffix(".exe.old_running")
    if old_backup.is_file():
        try:
            old_backup.unlink()
        except Exception:
            pass

    try:
        os.replace(temp_patched, target_exe)
        print(f"[+] 成功直接写入 PE 资源: {target_exe.name}")
        return True
    except PermissionError:
        print(f"[*] 检测到 {target_exe.name} 正在运行，使用原子重命名热替换...")
        os.rename(target_exe, old_backup)
        os.rename(temp_patched, target_exe)
        print(f"[+] 成功热替换运行中的 {target_exe.name} (旧进程仍存活，新启动将完全生效)")
        return True
    except Exception as e:
        print(f"[-] 注入 PE 图标异常: {e}")
        if temp_patched.is_file():
            temp_patched.unlink(missing_ok=True)
        return False


def detect_wt_session() -> str:
    """返回当前进程所在的 Windows Terminal 会话 ID（WT_SESSION 由 WT 注入给所有子进程），不在 WT 内则返回空串"""
    return os.environ.get("WT_SESSION", "").strip()


def ensure_wt_processes_closed(kill: bool) -> None:
    if not kill:
        return
    # 自身就在 WT 进程树内时，taskkill /T 会连当前会话一起终结；
    # 降级为不杀进程，让上层转走“原子重命名热替换”路径。
    if detect_wt_session():
        print("[!] 当前进程运行在 Windows Terminal 会话内，已跳过强制关闭以免终止自身会话。")
        print("    PE 注入将改用原子重命名热替换路径，无需关闭终端。")
        return
    closed: list[str] = []
    for name in ("WindowsTerminal.exe", "wt.exe"):
        result = subprocess.run(
            ["taskkill", "/F", "/IM", name, "/T"],
            capture_output=True,
            text=True,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        if result.returncode == 0:
            closed.append(name)
    if closed:
        print(f"[*] 已强制关闭进程: {', '.join(closed)}")
        time.sleep(0.8)


def add_to_user_path(directory: Path) -> bool:
    dir_str = str(directory)
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, r"Environment", 0, winreg.KEY_READ | winreg.KEY_WRITE) as key:
            try:
                current_path, _ = winreg.QueryValueEx(key, "Path")
            except FileNotFoundError:
                current_path = ""
            paths = [p.strip() for p in current_path.split(";") if p.strip()]
            if dir_str not in paths:
                new_path = dir_str + (";" + current_path if current_path else "")
                winreg.SetValueEx(key, "Path", 0, winreg.REG_EXPAND_SZ, new_path)
                try:
                    HWND_BROADCAST = 0xFFFF
                    WM_SETTINGCHANGE = 0x001A
                    SMTO_ABORTIFHUNG = 0x0002
                    result = ctypes.c_ulong()
                    user32.SendMessageTimeoutW(
                        HWND_BROADCAST, WM_SETTINGCHANGE, 0, "Environment", SMTO_ABORTIFHUNG, 3000, ctypes.byref(result)
                    )
                except Exception:
                    pass
                print(f"[+] 已将 {dir_str} 添加至用户环境变量 Path 最顶端")
                return True
            else:
                return True
    except Exception as e:
        print(f"[-] 写入环境变量 PATH 失败: {e}")
        return False


def remove_from_user_path(directory: Path) -> bool:
    dir_str = str(directory)
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, r"Environment", 0, winreg.KEY_READ | winreg.KEY_WRITE) as key:
            try:
                current_path, _ = winreg.QueryValueEx(key, "Path")
            except FileNotFoundError:
                return True
            paths = [p.strip() for p in current_path.split(";") if p.strip()]
            if dir_str in paths:
                new_paths = [p for p in paths if p != dir_str]
                winreg.SetValueEx(key, "Path", 0, winreg.REG_EXPAND_SZ, ";".join(new_paths))
                try:
                    HWND_BROADCAST = 0xFFFF
                    WM_SETTINGCHANGE = 0x001A
                    SMTO_ABORTIFHUNG = 0x0002
                    result = ctypes.c_ulong()
                    user32.SendMessageTimeoutW(
                        HWND_BROADCAST, WM_SETTINGCHANGE, 0, "Environment", SMTO_ABORTIFHUNG, 3000, ctypes.byref(result)
                    )
                except Exception:
                    pass
                print(f"[+] 已从用户环境变量 Path 中移除: {dir_str}")
                return True
    except Exception as e:
        print(f"[-] 移除环境变量 PATH 失败: {e}")
        return False


def read_exe_version(exe_path: Path) -> str:
    """读取 PE 文件的 Win32 版本号（如 1.25.2609.30003），失败返回空串"""
    if os.name != "nt" or not exe_path.is_file():
        return ""
    try:
        version_dll = ctypes.WinDLL("version", use_last_error=True)
        size = version_dll.GetFileVersionInfoSizeW(str(exe_path), None)
        if not size:
            return ""
        buffer = ctypes.create_string_buffer(size)
        if not version_dll.GetFileVersionInfoW(str(exe_path), 0, size, buffer):
            return ""
        value = ctypes.c_void_p()
        length = ctypes.c_uint()
        if not version_dll.VerQueryValueW(buffer, "\\", ctypes.byref(value), ctypes.byref(length)):
            return ""
        info = ctypes.cast(value, ctypes.POINTER(VS_FIXEDFILEINFO)).contents
        if info.dwSignature != 0xFEEF04BD:
            return ""
        return "{}.{}.{}.{}".format(
            info.dwFileVersionMS >> 16,
            info.dwFileVersionMS & 0xFFFF,
            info.dwFileVersionLS >> 16,
            info.dwFileVersionLS & 0xFFFF,
        )
    except Exception:
        return ""


def parse_version_tuple(text: str) -> tuple[int, ...]:
    """把 1.25.2609.30003 之类的版本串解析为可比较的整数元组"""
    return tuple(int(chunk) for chunk in re.findall(r"\d+", text or ""))


def version_from_asset_name(asset_name: str) -> str:
    """从 Microsoft.WindowsTerminal_1.25.2733.0_x64.zip 中提取版本号"""
    matched = re.search(r"WindowsTerminal_([\d.]+)_", asset_name or "")
    return matched.group(1) if matched else ""


def query_latest_wt_release() -> tuple[str, str]:
    """查询 GitHub 最新稳定版免安装包，返回 (asset_name, download_url)；不可用时退化到固定推荐版"""
    try:
        request = urllib.request.Request(GITHUB_LATEST_WT_API, headers={"User-Agent": USER_AGENT})
        with urllib.request.urlopen(request, timeout=15) as response:
            data = json.loads(response.read().decode("utf-8"))
        for asset in data.get("assets", []):
            name = asset.get("name", "")
            if name.startswith(WT_ASSET_PREFIX) and name.endswith(WT_ASSET_SUFFIX) and "msixbundle" not in name:
                return name, asset.get("browser_download_url", "")
        print("[-] GitHub API 未返回可用的 x64 免安装包，改用固定推荐稳定版。")
    except Exception as e:
        print(f"[-] 查询 GitHub API 异常: {e}，改用固定推荐稳定版。")
    return FALLBACK_WT_ASSET, FALLBACK_WT_URL


def download_file(url: str, dest_path: Path, desc: str = "文件") -> bool:
    print(f"[*] 正在自动下载 {desc}...")
    print(f"    来源: {url}")
    dest_path.parent.mkdir(parents=True, exist_ok=True)
    urls = [url]
    if "github.com" in url:
        urls.append(f"https://ghproxy.net/{url}")

    headers = {"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"}
    for u in urls:
        try:
            req = urllib.request.Request(u, headers=headers)
            with urllib.request.urlopen(req, timeout=30) as resp, open(dest_path, "wb") as f:
                total_len = resp.headers.get("Content-Length")
                total = int(total_len) if total_len else 0
                downloaded = 0
                while True:
                    chunk = resp.read(65536)
                    if not chunk:
                        break
                    f.write(chunk)
                    downloaded += len(chunk)
                    if total > 0:
                        pct = downloaded / total * 100
                        print(f"\r    进度: {pct:.1f}% ({downloaded // 1024}/{total // 1024} KB)", end="", flush=True)
                print()
            print(f"[+] 下载完成: {dest_path}")
            return True
        except Exception as e:
            print(f"\n[-] 从 {u} 下载失败: {e}，尝试备用线路...")
    return False


def auto_setup_windows_terminal(custom_dir: str | None = None) -> Path | None:
    if custom_dir:
        target_dir = Path(custom_dir).resolve()
    elif Path(r"D:\Users\Softwares").is_dir() or Path("D:/").is_dir():
        target_dir = Path(r"D:\Users\Softwares\WindowsTerminal")
    else:
        target_dir = Path(os.path.expandvars(r"%LOCALAPPDATA%\Programs\WindowsTerminal"))

    print(f"[*] 准备安装 Windows Terminal 独立版至: {target_dir}")

    asset_name, download_url = query_latest_wt_release()

    temp_zip = target_dir.parent / (asset_name or "WindowsTerminal_x64.zip")
    if not download_file(download_url, temp_zip, "Windows Terminal 官方免安装包"):
        return None

    print(f"[*] 正在解压缩至 {target_dir}...")
    target_dir.mkdir(parents=True, exist_ok=True)
    try:
        with zipfile.ZipFile(temp_zip, "r") as zf:
            zf.extractall(target_dir)
        print(f"[+] 解压完成！")
    except Exception as e:
        print(f"[-] 解压失败: {e}")
        return None
    finally:
        try:
            temp_zip.unlink(missing_ok=True)
        except Exception:
            pass

    add_to_user_path(target_dir)
    return target_dir


def upgrade_install_dir(target_dir: Path, asset_name: str, download_url: str) -> bool:
    """下载新版并原子换入目标目录（暂存解压 + 完整性校验 + 目录级重命名，失败回滚保留旧版）

    前置条件：调用方必须先确认当前进程不在 Windows Terminal 会话内（参见 detect_wt_session）。
    目录级重命名要求 WT 完全退出，而 taskkill /T 会连带终结同树内的调用方会话。
    """
    staging_dir = target_dir.parent / (target_dir.name + UPGRADE_STAGING_SUFFIX)
    backup_dir = target_dir.parent / (target_dir.name + UPGRADE_BACKUP_SUFFIX)
    temp_zip = target_dir.parent / (asset_name or "WindowsTerminal_x64.zip")

    # 清理上一次失败残留下来的暂存/备份目录
    for residue in (staging_dir, backup_dir):
        if residue.exists():
            shutil.rmtree(residue, ignore_errors=True)

    if not download_file(download_url, temp_zip, f"Windows Terminal 免安装包 {asset_name}"):
        return False

    print(f"[*] 正在解压至暂存目录: {staging_dir}")
    try:
        staging_dir.mkdir(parents=True, exist_ok=True)
        with zipfile.ZipFile(temp_zip, "r") as zf:
            zf.extractall(staging_dir)
    except Exception as e:
        print(f"[-] 解压失败: {e}")
        shutil.rmtree(staging_dir, ignore_errors=True)
        return False
    finally:
        temp_zip.unlink(missing_ok=True)

    # 完整性校验：关键入口 exe 必须齐全，否则拒绝换入，避免装出半残版本
    missing = [name for name in UPGRADE_REQUIRED_FILES if not (staging_dir / name).is_file()]
    if missing:
        print(f"[-] 新版本包缺少关键文件 {missing}，已放弃换入，旧版本保持可用。")
        shutil.rmtree(staging_dir, ignore_errors=True)
        return False

    # 换入前必须关闭 WT：Windows 不允许重命名包含运行中 exe 的目录
    ensure_wt_processes_closed(True)

    try:
        target_dir.rename(backup_dir)
        staging_dir.rename(target_dir)
    except OSError as e:
        print(f"[-] 目录原子换入失败: {e}")
        if not target_dir.exists() and backup_dir.exists():
            backup_dir.rename(target_dir)
            print("[*] 已回滚至旧版本，环境保持可用。")
        shutil.rmtree(staging_dir, ignore_errors=True)
        return False

    shutil.rmtree(backup_dir, ignore_errors=True)

    # 不变式：整目录换入天然重置 Images/*.ico.bak（旧目录连同旧版备份一起被丢弃），
    # 因此 --revert 只会还原与当前版本匹配的原版图标；若日后改为逐文件替换，必须显式清 bak。
    return True


def resolve_icon_path(style: str | None, custom_icon: str | None) -> Path:
    if custom_icon:
        p = Path(custom_icon).resolve()
        if not p.is_file():
            raise FileNotFoundError(f"未找到指定的自定义 ICO 文件: {p}")
        return p

    style_map = get_available_styles()
    target_style = style or "pure_dark"
    if target_style in style_map:
        return style_map[target_style]

    if "pure_dark" in style_map:
        return style_map["pure_dark"]
    if "titanium" in style_map:
        return style_map["titanium"]

    if style_map:
        return list(style_map.values())[0]

    raise ValueError(f"未知图标款式: {target_style}。可用款式: {list(style_map.keys())}")


def find_target_exes(custom_dir: str | None = None, custom_exe: str | None = None) -> list[Path]:
    if custom_exe:
        p = Path(custom_exe).resolve()
        if p.is_file():
            companion = p.parent / "WindowsTerminal.exe" if p.name.lower() == "wt.exe" else p.parent / "wt.exe"
            return [p, companion] if companion.is_file() else [p]

    dirs = [Path(custom_dir).resolve()] if custom_dir else DEFAULT_TARGET_DIRS
    exes: list[Path] = []
    for base_dir in dirs:
        if base_dir.is_dir():
            main_exe = base_dir / "WindowsTerminal.exe"
            wt_exe = base_dir / "wt.exe"
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
            companion = p.parent / "WindowsTerminal.exe"
            if companion.is_file():
                exes.append(companion)
            exes.append(p)
    return exes


def apply_app_paths(wt_exe_path: str) -> None:
    wt_dir = str(Path(wt_exe_path).parent)
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, APP_PATHS_KEY, 0, winreg.KEY_READ) as k:
            cur_val, _ = winreg.QueryValueEx(k, "")
            try:
                cur_path, _ = winreg.QueryValueEx(k, "Path")
            except Exception:
                cur_path = ""
    except Exception:
        cur_val = ""
        cur_path = ""

    state = load_state()
    if "original_app_paths_wt" not in state:
        state["original_app_paths_wt"] = cur_val or r"C:\Program Files\WindowsApps\Microsoft.WindowsTerminal_1.24.12741.0_x64__8wekyb3d8bbwe\wt.exe"
        state["original_app_paths_wt_path"] = cur_path or r"C:\Program Files\WindowsApps\Microsoft.WindowsTerminal_1.24.12741.0_x64__8wekyb3d8bbwe"
        save_state(state)

    try:
        with winreg.CreateKey(winreg.HKEY_CURRENT_USER, APP_PATHS_KEY) as k:
            winreg.SetValueEx(k, "", 0, winreg.REG_SZ, wt_exe_path)
            winreg.SetValueEx(k, "Path", 0, winreg.REG_SZ, wt_dir)
        print(f"[+] 已接管注册表 App Paths wt.exe -> {wt_exe_path} (含 Path -> {wt_dir})")
    except Exception as e:
        print(f"[-] 配置 App Paths 失败: {e}")


def revert_app_paths() -> None:
    state = load_state()
    orig = state.get("original_app_paths_wt")
    orig_path = state.get("original_app_paths_wt_path")
    if orig:
        try:
            with winreg.CreateKey(winreg.HKEY_CURRENT_USER, APP_PATHS_KEY) as k:
                winreg.SetValueEx(k, "", 0, winreg.REG_SZ, orig)
                if orig_path:
                    winreg.SetValueEx(k, "Path", 0, winreg.REG_SZ, orig_path)
            print(f"[+] 已恢复注册表 App Paths wt.exe -> {orig}")
        except Exception as e:
            print(f"[-] 恢复 App Paths 失败: {e}")


def get_defterm() -> tuple[str, str]:
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, CONSOLE_STARTUP_KEY, 0, winreg.KEY_READ) as k:
            c, _ = winreg.QueryValueEx(k, "DelegationConsole")
            t, _ = winreg.QueryValueEx(k, "DelegationTerminal")
            return str(c), str(t)
    except Exception:
        return "", ""


def apply_defterm(mode: str = "conhost") -> None:
    guid = GUID_CONHOST if mode == "conhost" else (GUID_WT if mode == "wt" else GUID_AUTO)
    state = load_state()
    cur_c, cur_t = get_defterm()
    if "original_defterm_console" not in state and cur_c:
        state["original_defterm_console"] = cur_c
        state["original_defterm_terminal"] = cur_t
        save_state(state)

    try:
        with winreg.CreateKey(winreg.HKEY_CURRENT_USER, CONSOLE_STARTUP_KEY) as k:
            winreg.SetValueEx(k, "DelegationConsole", 0, winreg.REG_SZ, guid)
            winreg.SetValueEx(k, "DelegationTerminal", 0, winreg.REG_SZ, guid)
        mode_desc = (
            "Windows 控制台主机 (conhost - 阻断静默脚本弹窗与焦点抢占，强烈推荐)"
            if mode == "conhost"
            else ("Windows Terminal" if mode == "wt" else "让 Windows 决定 (系统默认)")
        )
        print(f"[+] 已将系统默认终端 (DefTerm) 配置为: {mode_desc} (GUID: {guid})")
    except Exception as e:
        print(f"[-] 配置 DefTerm 失败: {e}")


def revert_defterm() -> None:
    state = load_state()
    orig_c = state.get("original_defterm_console")
    orig_t = state.get("original_defterm_terminal")
    if orig_c and orig_t:
        try:
            with winreg.CreateKey(winreg.HKEY_CURRENT_USER, CONSOLE_STARTUP_KEY) as k:
                winreg.SetValueEx(k, "DelegationConsole", 0, winreg.REG_SZ, orig_c)
                winreg.SetValueEx(k, "DelegationTerminal", 0, winreg.REG_SZ, orig_t)
            print(f"[+] 已恢复系统默认终端 (DefTerm) 为原值: {orig_t}")
        except Exception as e:
            print(f"[-] 恢复 DefTerm 失败: {e}")


def update_shortcut(lnk_path: Path, ico_target: str, exe_target: str, desc: str = "") -> bool:
    user_home = str(Path.home())
    ps_cmd = f"""
$wsh = New-Object -ComObject WScript.Shell
$sc = $wsh.CreateShortcut('{str(lnk_path)}')
$sc.TargetPath = '{exe_target}'
$sc.WorkingDirectory = '{user_home}'
$sc.Description = '{desc}'
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


def get_nilesoft_dir() -> Path | None:
    for d in NILESOFT_CANDIDATE_DIRS:
        if d.is_dir():
            return d
    return None


def apply_nilesoft(exe_target: str) -> None:
    nilesoft_dir = get_nilesoft_dir()
    if not nilesoft_dir:
        return

    term_nss = nilesoft_dir / "terminal.nss"
    mod_nss = nilesoft_dir / "modify.nss"

    for f in [term_nss, mod_nss]:
        if f.is_file():
            bak = f.with_suffix(".nss.bak")
            if not bak.is_file():
                try:
                    shutil.copy2(f, bak)
                    print(f"[+] 已安全备份 Nilesoft Shell 配置: {bak.name}")
                except Exception as e:
                    print(f"[-] 备份 {f.name} 失败: {e}")

    safe_target = exe_target.replace("\\", "\\\\")

    if term_nss.is_file():
        try:
            content = term_nss.read_text(encoding="utf-8")
            pat = r"(item\(where=package\.exists\(\"WindowsTerminal\"\) title=title\.Windows_Terminal tip=tip_run_admin admin=has_admin image=')(.*?)(' cmd=')(.*?)(' arg=)"
            if re.search(pat, content):
                content = re.sub(pat, rf"\g<1>{safe_target}\g<3>{safe_target}\g<5>", content)
                term_nss.write_text(content, encoding="utf-8", newline="\n")
                print("[+] 已更新 Nilesoft Shell terminal.nss 二级终端菜单命令与图标")
        except Exception as e:
            print(f"[-] 更新 terminal.nss 失败: {e}")

    if mod_nss.is_file():
        try:
            content = mod_nss.read_text(encoding="utf-8")
            patch_snippet = f"""
// Hide default Store WT item and replace with custom WT launcher
modify(mode=mode.multiple find='在终端中打开|open in terminal' vis=vis.remove)
item(type='dir|drive|back' title='在终端中打开(T)' pos=4 image='{safe_target}' cmd='{safe_target}' arg='-d "@sel.path\\."')
"""
            old_rule_pat = r"// (?:Custom Windows Terminal Icon|Hide default Store WT item)[^\n]*\n(?:(?:modify|item)\([^\n]+\)\n?)+"
            if re.search(old_rule_pat, content):
                content = re.sub(old_rule_pat, "", content).rstrip() + "\n" + patch_snippet
            elif "在终端中打开" not in content and "open in terminal" not in content:
                content += patch_snippet
            else:
                content = content.rstrip() + "\n" + patch_snippet

            mod_nss.write_text(content, encoding="utf-8", newline="\n")
            print("[+] 已更新 Nilesoft Shell modify.nss 顶层右键菜单独立版接管指令")
        except Exception as e:
            print(f"[-] 更新 modify.nss 失败: {e}")


def revert_nilesoft() -> None:
    nilesoft_dir = get_nilesoft_dir()
    if not nilesoft_dir:
        return

    for name in ["terminal.nss", "modify.nss"]:
        target = nilesoft_dir / name
        bak = nilesoft_dir / f"{name}.bak"
        if bak.is_file():
            try:
                shutil.copy2(bak, target)
                bak.unlink()
                print(f"[+] 已无损还原 Nilesoft Shell 配置文件: {name}")
            except Exception as e:
                print(f"[-] 还原 {name} 失败: {e}")


def sync_images_contrast(wt_dir: Path, ico_path: Path) -> None:
    images_dir = wt_dir / "Images"
    if not images_dir.is_dir():
        return
    for name in ["terminal_contrast-black.ico", "terminal_contrast-white.ico"]:
        dst = images_dir / name
        bak = images_dir / f"{name}.bak"
        if dst.is_file() and not bak.is_file():
            try:
                shutil.copy2(dst, bak)
            except Exception:
                pass
        try:
            shutil.copy2(ico_path, dst)
            print(f"[+] 已同步覆盖 Images/{name}")
        except Exception as e:
            print(f"[-] 同步 Images/{name} 异常: {e}")


def revert_images_contrast(wt_dir: Path) -> None:
    images_dir = wt_dir / "Images"
    if not images_dir.is_dir():
        return
    for name in ["terminal_contrast-black.ico", "terminal_contrast-white.ico"]:
        dst = images_dir / name
        bak = images_dir / f"{name}.bak"
        if bak.is_file():
            try:
                shutil.copy2(bak, dst)
                bak.unlink()
                print(f"[+] 已还原 Images/{name}")
            except Exception:
                pass


def refresh_live_windows(ico_path: Path) -> None:
    """向当前已打开的 Windows Terminal 窗口发送 WM_SETICON 即时热刷新"""
    WM_SETICON = 0x0080
    h_big = user32.LoadImageW(None, str(ico_path), 1, 32, 32, 0x00000010)
    h_small = user32.LoadImageW(None, str(ico_path), 1, 16, 16, 0x00000010)

    def enum_desk(hwnd, lParam):
        cls_buff = ctypes.create_unicode_buffer(256)
        user32.GetClassNameW(hwnd, cls_buff, 256)
        if "cascadia_hosting" in cls_buff.value.lower():
            user32.SendMessageW(hwnd, WM_SETICON, 1, h_big)
            user32.SendMessageW(hwnd, WM_SETICON, 0, h_small)
        return True

    WNDENUMPROC = ctypes.WINFUNCTYPE(ctypes.c_bool, wintypes.HWND, wintypes.LPARAM)
    hDesk = user32.OpenDesktopW("Default", 0, False, 0x0040 | 0x0001)
    if hDesk:
        user32.EnumDesktopWindows(hDesk, WNDENUMPROC(enum_desk), 0)
        user32.CloseDesktop(hDesk)


def rebuild_icon_cache_and_restart_explorer(shortcuts: list[Path], ico_path: Path | None = None) -> None:
    print("[*] 正在执行 Windows 11 深度离线图标缓存重构与 Explorer 重启...")
    now = time.time()
    for s in shortcuts:
        try:
            if s.is_file():
                os.utime(s, (now, now))
        except Exception:
            pass

    # 1. 停止 Explorer
    subprocess.run(["powershell", "-NoProfile", "-Command", "Stop-Process -Name explorer -Force"], check=False)
    time.sleep(1.0)

    # 2. 彻底离线删除缓存数据库
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

    print(f"[+] 已离线清除 {deleted_count} 个 Windows 图标缓存数据库！")

    # 3. 启动 Explorer
    subprocess.run(["powershell", "-NoProfile", "-Command", "Start-Process explorer.exe"], check=False)
    time.sleep(1.5)

    # 4. 广播外壳变更通知
    SHCNE_ASSOCCHANGED = 0x08000000
    SHCNF_FLUSH = 0x1000
    shell32.SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_FLUSH, None, None)
    print("[+] 已广播 Windows 外壳与图标刷新通知 (SHChangeNotify FLUSH)")

    if ico_path:
        refresh_live_windows(ico_path)


def apply_patch(
    style: str | None,
    custom_icon: str | None,
    target_dir: str | None,
    target_exe: str | None,
    restart_exp: bool,
    kill: bool = False,
    auto_setup: bool = False,
    defterm: str | None = None,
) -> int:
    ico_path = resolve_icon_path(style, custom_icon)
    print(f"[*] 选定定制图标: {ico_path.name} (路径: {ico_path})")

    ensure_wt_processes_closed(kill)

    target_exes = find_target_exes(target_dir, target_exe)
    if target_exes:
        if auto_setup:
            print("[!] 已检测到现有独立版安装：--auto-setup 只在目录为空时联网部署，本次不会升级版本。")
            print("    如需升级到 GitHub 最新稳定版，请改用 --update。")
    else:
        if auto_setup:
            print("[*] 正在全自动下载部署 Windows Terminal 独立版...")
            installed_dir = auto_setup_windows_terminal(target_dir)
            if installed_dir:
                target_exes = find_target_exes(str(installed_dir))

        if not target_exes:
            print("[-] 未定位到免安装独立版 WindowsTerminal，请确认解压目录。")
            print("💡 提示：在新电脑上可直接传入 --auto-setup 参数，脚本将全自动从 GitHub 下载最新独立版并完成全链路部署。")
            return 1

    wt_base_dir = target_exes[0].parent
    for exe in target_exes:
        if exe.name.lower() == "windowsterminal.exe":
            safe_patch_pe_binary(exe, ico_path, [101, 102, 103])
        else:
            safe_patch_pe_binary(exe, ico_path, [101])

    # 同步 Images/
    sync_images_contrast(wt_base_dir, ico_path)

    main_wt = str(target_exes[0])
    wt_bin = str(target_exes[1]) if len(target_exes) > 1 else main_wt

    # 1. 注册表 App Paths
    apply_app_paths(wt_bin)

    # 2. 系统默认终端 DefTerm (可选设置，conhost 阻断静默脚本弹窗抢焦点)
    if defterm:
        apply_defterm(defterm)

    # 3. 快捷方式 (仅管理开始菜单，严禁强写 User Pinned TaskBar 以防任务栏注册表脱节致使固定功能失效)
    updated_shortcuts: list[Path] = []
    orphaned_taskbar_lnk = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar\Windows Terminal.lnk"))
    if orphaned_taskbar_lnk.is_file():
        try:
            orphaned_taskbar_lnk.unlink()
            print(f"[+] 已清理冲突的孤立任务栏快捷方式 (恢复原生固定机制): {orphaned_taskbar_lnk.name}")
        except Exception:
            pass

    start_menu_lnk = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Windows\Start Menu\Programs\Windows Terminal.lnk"))
    if update_shortcut(start_menu_lnk, str(ico_path), main_wt, "Windows Terminal"):
        updated_shortcuts.append(start_menu_lnk)
        print(f"[+] 已同步开始菜单快捷方式: {start_menu_lnk}")

    # 3. Nilesoft Shell
    apply_nilesoft(main_wt)

    # 记录安装状态
    state = load_state()
    state["installed_dir_wt"] = str(wt_base_dir)
    state["style_wt"] = ico_path.stem
    version_source = next(
        (exe for exe in target_exes if exe.name.lower() == UPGRADE_MAIN_EXE.lower()), target_exes[0]
    )
    state["version_wt"] = read_exe_version(version_source) or state.get("version_wt", "")
    save_state(state)

    # 4. 外壳与缓存刷新
    if restart_exp:
        rebuild_icon_cache_and_restart_explorer(updated_shortcuts, ico_path)
    else:
        now = time.time()
        for s in updated_shortcuts:
            try:
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
        SHCNE_ASSOCCHANGED = 0x08000000
        SHCNF_FLUSH = 0x1000
        shell32.SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_FLUSH, None, None)
        refresh_live_windows(ico_path)

    print("\n" + "=" * 70)
    print("✅ Windows Terminal 定制与全链路守护已成功应用！")
    print(f"   生效图标: {ico_path.name}")
    print(f"   程序目标: {main_wt}")
    print("=" * 70)
    return 0


def update_windows_terminal(
    style: str | None,
    custom_icon: str | None,
    target_dir: str | None,
    restart_exp: bool,
    defterm: str | None = None,
) -> int:
    """幂等升级已部署的独立版：已是最新则直接返回 0；升级成功后自动重放全链路定制"""
    state = load_state()
    if target_dir:
        install_dir = Path(target_dir).resolve()
    else:
        recorded = state.get("installed_dir_wt")
        install_dir = Path(recorded).resolve() if recorded else DEFAULT_TARGET_DIRS[0]

    local_exe = install_dir / UPGRADE_MAIN_EXE
    if not local_exe.is_file():
        print(f"[-] 未发现已部署的独立版终端: {install_dir}")
        print("💡 首次部署请使用 --auto-setup；安装目录不同时请用 --target-dir 指定。")
        return 1

    local_version = read_exe_version(local_exe)
    asset_name, download_url = query_latest_wt_release()
    remote_version = version_from_asset_name(asset_name)
    print(f"[*] 本地版本: {local_version or '未知'}    远端版本: {remote_version or '未知'}")

    if local_version and remote_version and parse_version_tuple(remote_version) <= parse_version_tuple(local_version):
        print(f"[=] 已是最新版本 v{local_version}，无需升级。")
        return 0

    if not remote_version:
        print("[!] 无法比对远端版本号，将按请求执行覆盖升级。")

    # 安全闸门：升级必须关闭 WT 并重命名其安装目录，而若本进程就在某个 WT 窗口的进程树内，
    # taskkill /T 会连当前会话一起杀死。故在下载之前就拦下，并给出可执行的绕行方式。
    wt_session = detect_wt_session()
    if wt_session:
        print(f"[!] 检测到当前进程运行在 Windows Terminal 会话内 (WT_SESSION={wt_session})。")
        print("    升级需关闭全部 Windows Terminal 窗口，而本进程正属于其中一个窗口的进程树，")
        print("    继续执行会强杀当前会话自身、导致升级流程中途夭折。已中止升级，未做任何修改。")
        print("    请改用以下方式之一重跑：")
        print("      1) 先手动关闭所有 Windows Terminal 窗口，再从 Windows 控制台主机（conhost）执行；")
        print("      2) 在 Win+R 打开的独立终端窗口中执行本命令。")
        print("    注：本条仅拦住“升级”（需重命名安装目录）；日常图标定制不受影响。")
        return 1

    if not upgrade_install_dir(install_dir, asset_name, download_url):
        print("[-] 升级失败，旧版本保持可用。")
        return 1

    print(f"[+] 已升级: v{local_version or '未知'} → v{read_exe_version(local_exe) or '未知'}")

    # 复刻既有款式：优先命令行显式指定，其次沿用 state 记录的款式，避免把用户款式改回默认
    effective_style = style or state.get("style_wt") or "pure_dark"
    print(f"[*] 正在重放定制与全链路守卫（图标款式: {effective_style}）...")
    code = apply_patch(
        effective_style,
        custom_icon,
        str(install_dir),
        None,
        restart_exp,
        False,
        False,
        defterm,
    )
    if code != 0:
        print("[-] 定制重放失败，请手动重新执行 patch_wt_icon.py 复核。")
    return code


def revert_autostart() -> None:
    """清理 Win32 开机自启动注册表项。"""
    run_key = r"Software\Microsoft\Windows\CurrentVersion\Run"
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, run_key, 0, winreg.KEY_SET_VALUE) as k:
            winreg.DeleteValue(k, "WindowsTerminalCustom")
            print("[+] 已清理开机自启动项: WindowsTerminalCustom")
    except FileNotFoundError:
        pass
    except Exception as e:
        print(f"[-] 清理开机自启动项失败: {e}")


def revert_patch(restart_exp: bool = False, remove_path: bool = False) -> int:
    print("[*] 正在执行 Windows Terminal 定制回退与 100% 官方原生复原...")

    # 1. 还原 App Paths
    revert_app_paths()

    # 2. 还原系统默认终端 DefTerm
    revert_defterm()

    # 3. 清理开机自启动
    revert_autostart()

    # 4. 还原 Nilesoft Shell
    revert_nilesoft()

    # 4. 还原 Images 对比度图标
    state = load_state()
    installed_dir = state.get("installed_dir_wt")
    if installed_dir:
        revert_images_contrast(Path(installed_dir))

    # 4. 移除快捷方式
    taskbar_lnk = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar\Windows Terminal.lnk"))
    start_menu_lnk = Path(os.path.expandvars(r"%APPDATA%\Microsoft\Windows\Start Menu\Programs\Windows Terminal.lnk"))
    touched: list[Path] = []
    for lnk in [taskbar_lnk, start_menu_lnk]:
        if lnk.is_file():
            try:
                lnk.unlink()
                print(f"[+] 已移除快捷方式: {lnk.name}")
            except Exception as e:
                print(f"[-] 移除快捷方式失败 {lnk.name}: {e}")

    # 5. 可选移除 PATH
    if remove_path and installed_dir:
        remove_from_user_path(Path(installed_dir))

    if restart_exp:
        rebuild_icon_cache_and_restart_explorer(touched)
    else:
        try:
            subprocess.run(
                ["ie4uinit.exe", "-show"],
                check=False,
                creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
            )
        except Exception:
            pass
        SHCNE_ASSOCCHANGED = 0x08000000
        SHCNF_FLUSH = 0x1000
        shell32.SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_FLUSH, None, None)

    print("\n" + "=" * 70)
    print("✅ 已彻底还原为官方原生状态！")
    print("=" * 70)
    return 0


def main():
    parser = argparse.ArgumentParser(description="Windows Terminal 任务栏图标定制与全链路恢复守卫")
    parser.add_argument("--style", type=str, default=None, help="图标风格 (pure_dark, titanium, underlit_chrome, dark_blue, cyan_matrix)；缺省沿用 state.json 记录的款式")
    parser.add_argument("--custom-icon", type=str, default=None, help="自定义 .ico 文件绝对路径")
    parser.add_argument("--target-exe", type=str, default=None, help="目标 EXE 绝对路径")
    parser.add_argument("--target-dir", type=str, default=None, help="Windows Terminal 免安装独立版解压目录")
    parser.add_argument("--kill", action="store_true", help="若 Windows Terminal 正在运行，先强制关闭再注入（默认采用原子重命名热替换）")
    parser.add_argument("--restart-explorer", action="store_true", help="修改完成后自动离线重建图标缓存并重启 Explorer")
    parser.add_argument("--auto-setup", action="store_true", help="新电脑全自动部署模式：自动从 GitHub 下载最新免安装终端，并自动配置 PATH（仅目录为空时联网部署）")
    parser.add_argument("--update", action="store_true", help="幂等升级已部署的独立版到 GitHub 最新稳定版（已是最新则直接退出 0），随后自动重放全链路定制")
    parser.add_argument("--revert", "--restore", dest="revert", action="store_true", help="一键彻底撤销修改并还原为系统官方原生状态")
    parser.add_argument("--remove-path", action="store_true", help="复原时一并从用户环境变量 Path 中移除独立版目录")
    parser.add_argument("--list-styles", action="store_true", help="列出全部可用图标风格")
    parser.add_argument("--defterm", type=str, choices=["conhost", "wt", "auto"], default=None, help="配置系统默认终端 (DefTerm)：conhost (推荐开发者环境，阻断静默脚本弹窗抢焦点), wt, auto")

    args = parser.parse_args()

    if args.list_styles:
        styles = get_available_styles()
        print("可用图标风格列表:")
        for name, p in styles.items():
            print(f"  - {name:<16} ({p.name})")
        return

    if args.defterm and len(sys.argv) <= 3:
        apply_defterm(args.defterm)
        return

    if args.update:
        sys.exit(
            update_windows_terminal(
                args.style,
                args.custom_icon,
                args.target_dir,
                args.restart_explorer,
                args.defterm,
            )
        )

    if args.revert:
        sys.exit(revert_patch(args.restart_explorer, args.remove_path))

    sys.exit(apply_patch(
        args.style,
        args.custom_icon,
        args.target_dir,
        args.target_exe,
        args.restart_explorer,
        args.kill,
        args.auto_setup,
        args.defterm,
    ))


if __name__ == "__main__":
    main()
