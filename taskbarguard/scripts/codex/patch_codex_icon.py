# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pillow>=11.0",
# ]
# ///
r"""
========================================================================================
Codex（OpenAI Codex / ChatGPT 桌面版，MSIX 商店包）任务栏图标精细化修改与更新恢复脚本
========================================================================================
【为什么需要单独的 MSIX 通道】
Codex 是 Microsoft Store 打包应用（Package: OpenAI.Codex），Windows 任务栏对打包应用
忽略窗口 / EXE 图标，只认包清单 assets 下的图标资产。因此本脚本不走 rcedit 改 EXE，
而是直接替换包内 35 个图标 PNG（含 targetsize unplated / lightunplated 全尺寸）。

【核心能力】
1. 自动定位当前已安装包（每次现取，不写死版本目录），适配商店自动更新后的路径变化；
2. 目标资产先渲染到 %TEMP% 暂存，再通过 UAC 自提权落盘；未变化时完全跳过提权；
3. 落盘前按 PackageFullName 备份原始 PNG 与哈希清单，--restore 可无损还原；
4. 只尝试以 SeRestorePrivilege 打开写句柄，不改 ACL；Windows 11 24H2/25H2 对商店包
   启用了文件对象级完整性保护（bindflt/UCPD/WdFilter 等），会拒绝写入，脚本会打印
   明确诊断并退出，不留下 ACL 副作用；
5. 落盘后刷新 shell 图标缓存；--restart-explorer 可选深度刷新，--kill 自动关闭/重开应用。

【使用】
    uv run scripts/codex/patch_codex_icon.py --style bright_titanium --kill
    uv run scripts/codex/patch_codex_icon.py --restore
    uv run scripts/codex/patch_codex_icon.py --style underlit_chrome --dry-run
"""

from __future__ import annotations

import argparse
import ctypes
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import xml.etree.ElementTree as ET
from ctypes import wintypes
from datetime import datetime, timezone
from pathlib import Path

# Pillow 只在渲染阶段按需导入：提权子进程仅拷贝文件，不依赖第三方库，
# 因此即使 sys.executable 不是 uv 脚本环境也能正常完成落盘。
if sys.platform == "win32":
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "buffer") and stream.encoding and stream.encoding.lower() not in ("utf-8", "utf8"):
            try:
                stream.reconfigure(encoding="utf-8", errors="replace")
            except Exception:
                pass

SCRIPT_DIR = Path(__file__).resolve().parent
ICON_DIR = SCRIPT_DIR / "custom_icons"
SCRIPT_PATH = Path(__file__).resolve()

PACKAGE_NAME = "OpenAI.Codex"
DEFAULT_STYLE = "bright_titanium"
STYLE_FILES = ("bright_titanium", "underlit_chrome", "pure_dark")

# 包 assets 下需要替换的固定图标（含清单 Logo / 44x44 / 150x150 与 200% 变体）
FIXED_ASSETS = (
    "icon.png",
    "Square44x44Logo.png",
    "Square44x44Logo.scale-200.png",
    "Square150x150Logo.png",
    "Square150x150Logo.scale-200.png",
)
TARGETSIZE_GLOB = "Square44x44Logo.targetsize-*_altform-*.png"

DATA_ROOT = Path(os.environ.get("APPDATA", Path.home())) / "language_projects" / "taskbarguard"
BACKUP_ROOT = DATA_ROOT / "codex-backups"
STATE_PATH = DATA_ROOT / "codex-state.json"

# 提权相关
SW_HIDE = 0
SEE_MASK_NOCLOSEPROCESS = 0x00000040
INFINITE = 0xFFFFFFFF
ERROR_CANCELLED = 1223


class ShellExecuteInfoW(ctypes.Structure):
    _fields_ = [
        ("cbSize", wintypes.DWORD),
        ("fMask", ctypes.c_ulong),
        ("hwnd", wintypes.HWND),
        ("lpVerb", wintypes.LPCWSTR),
        ("lpFile", wintypes.LPCWSTR),
        ("lpParameters", wintypes.LPCWSTR),
        ("lpDirectory", wintypes.LPCWSTR),
        ("nShow", ctypes.c_int),
        ("hInstApp", wintypes.HINSTANCE),
        ("lpIDList", ctypes.c_void_p),
        ("lpClass", wintypes.LPCWSTR),
        ("hkeyClass", wintypes.HKEY),
        ("dwHotKey", wintypes.DWORD),
        ("hIcon", wintypes.HANDLE),
        ("hProcess", wintypes.HANDLE),
    ]


class Luid(ctypes.Structure):
    _fields_ = [("LowPart", wintypes.DWORD), ("HighPart", wintypes.LONG)]


class LuidAndAttributes(ctypes.Structure):
    _fields_ = [("Luid", Luid), ("Attributes", wintypes.DWORD)]


class TokenPrivileges(ctypes.Structure):
    _fields_ = [("PrivilegeCount", wintypes.DWORD), ("Privileges", LuidAndAttributes * 1)]


TOKEN_ADJUST_PRIVILEGES = 0x0020
TOKEN_QUERY = 0x0008
SE_PRIVILEGE_ENABLED = 0x00000002
FILE_FLAG_BACKUP_SEMANTICS = 0x02000000
GENERIC_WRITE = 0x40000000
FILE_SHARE_ALL = 0x00000007
OPEN_EXISTING = 3


_LOG_HANDLE = None


def log(message: str) -> None:
    try:
        print(message, flush=True)
    except OSError:
        # 父 shell 管道被提前关闭（如 | head）时不打断补丁流程
        pass
    if _LOG_HANDLE is not None:
        _LOG_HANDLE.write(message + "\n")
        _LOG_HANDLE.flush()


def set_log_file(path: Path | None) -> None:
    global _LOG_HANDLE
    if path is None:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    _LOG_HANDLE = path.open("w", encoding="utf-8")


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def is_admin() -> bool:
    try:
        return bool(ctypes.windll.shell32.IsUserAnAdmin())
    except Exception:
        return False


def run_hidden(args: list[str]) -> subprocess.CompletedProcess:
    return subprocess.run(
        args,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
        creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
    )


def elevate(args: list[str]) -> int:
    """通过 UAC 以管理员身份重启自身，隐藏子控制台并等待其退出。"""
    log_path = Path(tempfile.gettempdir()) / f"taskbarguard-codex-{os.getpid()}.log"
    parameters = subprocess.list2cmdline([str(SCRIPT_PATH), "--elevated", "--log", str(log_path), *args])

    info = ShellExecuteInfoW()
    info.cbSize = ctypes.sizeof(ShellExecuteInfoW)
    info.fMask = SEE_MASK_NOCLOSEPROCESS
    info.lpVerb = "runas"
    info.lpFile = sys.executable
    info.lpParameters = parameters
    info.lpDirectory = str(SCRIPT_DIR)
    info.nShow = SW_HIDE

    shell32 = ctypes.WinDLL("shell32", use_last_error=True)
    kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    shell32.ShellExecuteExW.argtypes = [ctypes.POINTER(ShellExecuteInfoW)]
    shell32.ShellExecuteExW.restype = wintypes.BOOL
    kernel32.WaitForSingleObject.argtypes = [wintypes.HANDLE, wintypes.DWORD]
    kernel32.WaitForSingleObject.restype = wintypes.DWORD
    kernel32.GetExitCodeProcess.argtypes = [wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD)]
    kernel32.GetExitCodeProcess.restype = wintypes.BOOL
    kernel32.CloseHandle.argtypes = [wintypes.HANDLE]
    kernel32.CloseHandle.restype = wintypes.BOOL

    if not shell32.ShellExecuteExW(ctypes.byref(info)):
        code = ctypes.get_last_error()
        if code == ERROR_CANCELLED:
            log("[-] 已取消 UAC 提权，未做任何修改。")
        else:
            log(f"[-] 提权失败（Win32 错误码 {code}）。")
        return 3

    kernel32.WaitForSingleObject(info.hProcess, INFINITE)
    exit_code = wintypes.DWORD(0)
    kernel32.GetExitCodeProcess(info.hProcess, ctypes.byref(exit_code))
    kernel32.CloseHandle(info.hProcess)

    if log_path.is_file():
        content = log_path.read_text(encoding="utf-8", errors="replace").strip()
        if content:
            log(content)
        try:
            log_path.unlink()
        except OSError:
            pass
    return int(exit_code.value)


def find_package() -> dict | None:
    """读取当前已安装的 Codex MSIX 包信息（只读，无需管理员）。"""
    script = (
        "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; "
        f"$pkg = Get-AppxPackage -Name '{PACKAGE_NAME}' | Sort-Object Version -Descending | Select-Object -First 1; "
        "if (-not $pkg) { 'null' } else { "
        "$pkg | Select-Object Name, PackageFullName, PackageFamilyName, InstallLocation, Version | ConvertTo-Json -Compress }"
    )
    result = run_hidden(["powershell", "-NoProfile", "-NonInteractive", "-Command", script])
    if result.returncode != 0:
        raise RuntimeError(f"Get-AppxPackage 调用失败: {result.stderr.strip()}")
    raw = result.stdout.strip().splitlines()[-1] if result.stdout.strip() else "null"
    if raw == "null" or not raw:
        return None
    data = json.loads(raw)
    data["InstallLocation"] = str(data.get("InstallLocation", ""))
    data["Version"] = str(data.get("Version", ""))
    return data


def read_process_names(package_dir: Path) -> list[str]:
    manifest = package_dir / "AppxManifest.xml"
    names: list[str] = []
    if manifest.is_file():
        try:
            root = ET.parse(manifest).getroot()
            for application in root.iter():
                if not application.tag.endswith("Application"):
                    continue
                executable = application.attrib.get("Executable", "")
                entry = application.attrib.get("EntryPoint", "")
                if not executable or "FullTrustApplication" not in entry:
                    continue
                name = Path(executable.replace("\\", "/")).name
                if name.lower().endswith(".exe") and name not in names:
                    names.append(name)
        except ET.ParseError:
            pass
    if not names:
        names = ["ChatGPT.exe"]
    return names


def load_master(style: str | None, custom_icon: str | None) -> tuple[str, Path]:
    """返回 (款式名, 1024 母版图路径)。"""
    if custom_icon:
        path = Path(custom_icon).resolve()
        if not path.is_file():
            raise FileNotFoundError(f"自定义图标不存在: {path}")
        # Go CLI 传入的是 .ico（用于款式校验），优先使用同目录同名 1024 PNG 母版
        if path.suffix.lower() == ".ico":
            png_master = path.with_suffix(".png")
            if png_master.is_file():
                return (path.stem, png_master)
        return (path.stem, path)

    name = style or DEFAULT_STYLE
    png_path = ICON_DIR / f"{name}.png"
    ico_path = ICON_DIR / f"{name}.ico"
    if png_path.is_file():
        return (name, png_path)
    if ico_path.is_file():
        return (name, ico_path)
    raise FileNotFoundError(
        f"未找到款式 {name} 的图标（{png_path.name} / {ico_path.name}），可用款式: {', '.join(STYLE_FILES)}"
    )


def open_master(path: Path):
    from PIL import Image

    image = Image.open(path)
    if path.suffix.lower() == ".ico":
        ico = getattr(image, "ico", None)
        if ico is not None:
            image.size = max(ico.sizes())
        image.load()
    return image.convert("RGBA")


def collect_assets(package_dir: Path) -> list[tuple[str, int]]:
    """枚举需要替换的 assets 相对路径与实际像素尺寸。"""
    from PIL import Image

    assets_dir = package_dir / "assets"
    if not assets_dir.is_dir():
        raise FileNotFoundError(f"包内未找到 assets 目录: {assets_dir}")

    items: list[tuple[str, int]] = []
    for name in FIXED_ASSETS:
        path = assets_dir / name
        if not path.is_file():
            continue
        with Image.open(path) as image:
            items.append((name, image.width))

    for path in sorted(assets_dir.glob(TARGETSIZE_GLOB)):
        with Image.open(path) as image:
            items.append((path.name, image.width))

    if not items:
        raise FileNotFoundError("未在包内发现任何可替换的图标资产。")
    return items


def stage_images(master, assets: list[tuple[str, int]], stage_dir: Path) -> list[dict]:
    from PIL import Image

    stage_dir.mkdir(parents=True, exist_ok=True)
    staged: list[dict] = []
    for name, size in assets:
        target = stage_dir / name
        frame = master if master.size == (size, size) else master.resize((size, size), Image.Resampling.LANCZOS)
        frame.save(target, "PNG", optimize=True)
        staged.append({"name": name, "size": size, "path": str(target), "sha256": sha256(target)})
    return staged


def backup_dir_for(package_full_name: str) -> Path:
    return BACKUP_ROOT / package_full_name


def write_state(package: dict, style: str, files: list[dict]) -> None:
    STATE_PATH.parent.mkdir(parents=True, exist_ok=True)
    payload = {
        "package": package.get("PackageFullName", ""),
        "style": style,
        "appliedAt": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "files": {item["name"]: item["sha256"] for item in files},
    }
    STATE_PATH.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def enable_privilege(name: str) -> bool:
    """启用当前进程令牌中的指定特权（如 SeRestorePrivilege）。"""
    advapi32 = ctypes.WinDLL("advapi32", use_last_error=True)
    kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    advapi32.OpenProcessToken.argtypes = [wintypes.HANDLE, wintypes.DWORD, ctypes.POINTER(wintypes.HANDLE)]
    advapi32.OpenProcessToken.restype = wintypes.BOOL
    advapi32.LookupPrivilegeValueW.argtypes = [wintypes.LPCWSTR, wintypes.LPCWSTR, ctypes.POINTER(Luid)]
    advapi32.LookupPrivilegeValueW.restype = wintypes.BOOL
    advapi32.AdjustTokenPrivileges.argtypes = [
        wintypes.HANDLE,
        wintypes.BOOL,
        ctypes.POINTER(TokenPrivileges),
        wintypes.DWORD,
        wintypes.LPVOID,
        wintypes.LPVOID,
    ]
    advapi32.AdjustTokenPrivileges.restype = wintypes.BOOL
    kernel32.GetCurrentProcess.restype = wintypes.HANDLE
    kernel32.CloseHandle.argtypes = [wintypes.HANDLE]

    token = wintypes.HANDLE()
    if not advapi32.OpenProcessToken(
        kernel32.GetCurrentProcess(), TOKEN_ADJUST_PRIVILEGES | TOKEN_QUERY, ctypes.byref(token)
    ):
        return False
    luid = Luid()
    if not advapi32.LookupPrivilegeValueW(None, name, ctypes.byref(luid)):
        kernel32.CloseHandle(token)
        return False
    privileges = TokenPrivileges()
    privileges.PrivilegeCount = 1
    privileges.Privileges[0].Luid = luid
    privileges.Privileges[0].Attributes = SE_PRIVILEGE_ENABLED
    ctypes.set_last_error(0)
    ok = advapi32.AdjustTokenPrivileges(token, False, ctypes.byref(privileges), 0, None, None)
    error = ctypes.get_last_error()
    kernel32.CloseHandle(token)
    return bool(ok) and error == 0


def open_with_restore_privilege(path: Path) -> tuple[int, int]:
    """以 SeRestorePrivilege + FILE_FLAG_BACKUP_SEMANTICS 绕过 DACL 打开可写句柄。"""
    enable_privilege("SeRestorePrivilege")
    kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel32.CreateFileW.argtypes = [
        wintypes.LPCWSTR,
        wintypes.DWORD,
        wintypes.DWORD,
        wintypes.LPVOID,
        wintypes.DWORD,
        wintypes.DWORD,
        wintypes.HANDLE,
    ]
    kernel32.CreateFileW.restype = wintypes.HANDLE
    handle = kernel32.CreateFileW(
        str(path), GENERIC_WRITE, FILE_SHARE_ALL, None, OPEN_EXISTING, FILE_FLAG_BACKUP_SEMANTICS, None
    )
    error = ctypes.get_last_error()
    return int(handle or 0), error


def write_bytes_privileged(path: Path, data: bytes) -> tuple[bool, str]:
    """尝试以恢复特权写句柄覆盖文件内容（不改 ACL）；失败时返回明确诊断。"""
    handle_value, error = open_with_restore_privilege(path)
    if handle_value and handle_value != ctypes.c_void_p(-1).value:
        kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel32.WriteFile.argtypes = [
            wintypes.HANDLE,
            wintypes.LPCVOID,
            wintypes.DWORD,
            ctypes.POINTER(wintypes.DWORD),
            wintypes.LPVOID,
        ]
        kernel32.WriteFile.restype = wintypes.BOOL
        kernel32.SetEndOfFile.argtypes = [wintypes.HANDLE]
        kernel32.SetEndOfFile.restype = wintypes.BOOL
        kernel32.CloseHandle.argtypes = [wintypes.HANDLE]
        written = wintypes.DWORD(0)
        ok = kernel32.WriteFile(handle_value, data, len(data), ctypes.byref(written), None)
        write_error = ctypes.get_last_error()
        kernel32.SetEndOfFile(handle_value)
        kernel32.CloseHandle(handle_value)
        if ok and written.value == len(data):
            return True, "restore-privilege"
        return False, f"restore-privilege 写入失败 (win32={write_error}, written={written.value}/{len(data)})"

    if error == 5:
        return False, (
            "目标文件拒绝写入（Win32 5）。Windows 11 24H2/25H2 对商店包文件启用了"
            "文件对象级完整性保护，管理员 + SeRestorePrivilege 也无法覆盖；"
            "本模块仅适用于允许写入商店包的系统版本。"
        )
    return False, f"CreateFileW 失败 (win32={error})"


def backup_originals(package_dir: Path, assets: list[tuple[str, int]], package_full_name: str) -> None:
    destination = backup_dir_for(package_full_name)
    manifest_path = destination / "manifest.json"
    if manifest_path.is_file():
        return

    destination.mkdir(parents=True, exist_ok=True)
    entries = []
    for name, _ in assets:
        source = package_dir / "assets" / name
        target = destination / "assets" / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)
        entries.append({"name": name, "sha256": sha256(source), "size": source.stat().st_size})
    manifest = {
        "package": package_full_name,
        "createdAt": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "files": entries,
    }
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    log(f"[+] 已备份官方原始资产: {destination}")


def apply_stage(stage_manifest: Path) -> int:
    manifest = json.loads(stage_manifest.read_text(encoding="utf-8"))
    package = find_package()
    if not package:
        log(f"[-] 未找到已安装的 {PACKAGE_NAME} 包。")
        return 4
    if package["PackageFullName"] != manifest["package"]:
        log("[-] 安装包版本已变化，请重新执行命令（会在新版本上重新暂存）。")
        return 4

    package_dir = Path(package["InstallLocation"])
    log(f"[*] 目标包: {package['PackageFullName']}")
    log(f"[*] 安装目录: {package_dir}")
    log(f"[*] 提权状态: {'管理员' if is_admin() else '普通用户'}")

    assets = [(item["name"], item["size"]) for item in manifest["files"]]
    backup_originals(package_dir, assets, package["PackageFullName"])

    process_names = read_process_names(package_dir)
    if manifest.get("kill"):
        for name in process_names:
            run_hidden(["taskkill", "/F", "/IM", name])
            log(f"[*] 已关闭运行中的进程: {name}")
        time.sleep(0.8)

    written = 0
    methods: set[str] = set()
    for item in manifest["files"]:
        source = Path(item["path"])
        target = package_dir / "assets" / item["name"]
        if not target.is_file():
            log(f"[-] 跳过缺失资产: {item['name']}")
            continue
        if target.is_file() and sha256(target) == item["sha256"]:
            continue
        ok, detail = write_bytes_privileged(target, source.read_bytes())
        if not ok:
            log(f"[-] 写入失败: {item['name']} -> {detail}")
            return 1
        methods.add(detail)
        os.utime(target, None)
        if sha256(target) != item["sha256"]:
            log(f"[-] 写入校验失败: {item['name']}")
            return 1
        written += 1

    write_state(package, manifest.get("style", ""), manifest["files"])
    log(f"[+] 已写入 {written} 个图标资产（{len(manifest['files'])} 个目标，方式: {', '.join(sorted(methods)) or '无'}）")

    refresh_shell(package_dir, [item["name"] for item in manifest["files"]])

    log("")
    log("=" * 60)
    log("[SUCCESS] Codex 任务栏与桌面包图标补丁完成！")
    log(f"• 款式: {manifest.get('style', '')}")
    log(f"• 备份: {backup_dir_for(package['PackageFullName'])}")
    log("=" * 60)
    return 0


def refresh_shell(package_dir: Path, names: list[str]) -> None:
    for name in names:
        try:
            os.utime(package_dir / "assets" / name, None)
        except OSError:
            pass

    try:
        subprocess.run(
            ["ie4uinit.exe", "-show"],
            check=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
        )
        log("[+] 已调用 ie4uinit 刷新图标位图缓存")
    except OSError:
        pass

    try:
        ctypes.windll.shell32.SHChangeNotify(0x08000000, 0x1000, 0, 0)
        log("[+] 已广播 SHChangeNotify 外壳关联更新")
    except Exception as exc:
        log(f"[-] SHChangeNotify 调用异常: {exc}")


def restart_explorer() -> None:
    """仅在用户显式要求时执行；由非提权父进程调用，避免拉起管理员 Explorer。"""
    log("[*] 正在执行 Explorer 重启与图标缓存深度清理...")
    subprocess.run(["taskkill", "/F", "/IM", "explorer.exe"], check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    time.sleep(1.0)
    explorer_dir = Path(os.environ.get("LOCALAPPDATA", "")) / "Microsoft" / "Windows" / "Explorer"
    removed = 0
    for pattern in ("iconcache*.db", "thumbcache*.db"):
        for path in explorer_dir.glob(pattern):
            try:
                path.unlink()
                removed += 1
            except OSError:
                pass
    icon_cache = Path(os.environ.get("LOCALAPPDATA", "")) / "IconCache.db"
    if icon_cache.is_file():
        try:
            icon_cache.unlink()
            removed += 1
        except OSError:
            pass
    log(f"[+] 已清除 {removed} 个图标缓存文件")
    subprocess.run(["explorer.exe"], check=False)


def launch_package(package: dict) -> None:
    family = package.get("PackageFamilyName", "")
    if not family:
        return
    subprocess.run(["explorer.exe", f"shell:AppsFolder\\{family}!App"], check=False)


def restore_stage(package: dict, kill: bool = False) -> int:
    destination = backup_dir_for(package["PackageFullName"])
    manifest_path = destination / "manifest.json"
    if not manifest_path.is_file():
        log(f"[*] 当前版本没有历史备份，图标应已是官方原版: {package['PackageFullName']}")
        return 0

    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    package_dir = Path(package["InstallLocation"])

    if kill:
        for name in read_process_names(package_dir):
            run_hidden(["taskkill", "/F", "/IM", name])
            log(f"[*] 已关闭运行中的进程: {name}")
        time.sleep(0.8)

    restored = 0
    for entry in manifest["files"]:
        source = destination / "assets" / entry["name"]
        target = package_dir / "assets" / entry["name"]
        if not source.is_file() or not target.is_file():
            continue
        ok, detail = write_bytes_privileged(target, source.read_bytes())
        if not ok:
            log(f"[-] 还原失败: {entry['name']} -> {detail}")
            return 1
        os.utime(target, None)
        restored += 1

    log(f"[+] 已还原 {restored} 个官方原始图标资产")
    refresh_shell(package_dir, [entry["name"] for entry in manifest["files"]])
    if STATE_PATH.is_file():
        try:
            STATE_PATH.unlink()
        except OSError:
            pass
    log("[SUCCESS] Codex 已恢复官方原始图标！")
    return 0


def plan_targets(package_dir: Path, staged: list[dict]) -> tuple[int, int]:
    assets_dir = package_dir / "assets"
    matched = 0
    for item in staged:
        target = assets_dir / item["name"]
        if target.is_file() and sha256(target) == item["sha256"]:
            matched += 1
    return matched, len(staged) - matched


def finalize_parent(package: dict, relaunch: bool, restart: bool) -> None:
    """提权子进程结束后由父进程完成非特权收尾（重开应用 / 安全重启 Explorer）。"""
    if relaunch:
        launch_package(package)
        log("[*] 已重新启动 Codex")
    if restart:
        if is_admin():
            log("[!] 当前进程以管理员身份运行；为避免拉起管理员 Explorer，已跳过自动重启。")
            log("    请注销重登，或以普通权限再次执行 --restart-explorer。")
        else:
            restart_explorer()
    if not restart:
        log("[*] 若任务栏图标未立即刷新：执行 --restart-explorer，或取消/重新固定一次 Codex。")


def describe_restore(package: dict) -> int:
    destination = backup_dir_for(package["PackageFullName"])
    manifest_path = destination / "manifest.json"
    if not manifest_path.is_file():
        log(f"[*] 当前版本没有历史备份，图标应已是官方原版: {package['PackageFullName']}")
        return 0
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    log(f"[*] 将还原 {len(manifest['files'])} 个官方原始资产（来源: {destination}）")
    for entry in manifest["files"]:
        log(f"    - assets/{entry['name']}")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Codex（MSIX 商店包）任务栏图标修改与官方还原工具",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--style", type=str, help=f"款式名称（{' / '.join(STYLE_FILES)}），默认 {DEFAULT_STYLE}")
    parser.add_argument("--custom-icon", type=str, help="自定义 ICO / PNG 母版路径")
    parser.add_argument("--restore", "--revert", action="store_true", help="从当前版本备份还原官方原始图标")
    parser.add_argument("--kill", action="store_true", help="写入前关闭并写完后重新启动 Codex")
    parser.add_argument("--relaunch", action="store_true", help="写完后重新启动 Codex（不强制关闭）")
    parser.add_argument("--restart-explorer", action="store_true", help="写入后深度重建图标缓存并重启 Explorer")
    parser.add_argument("--dry-run", action="store_true", help="只输出将要修改的资产，不写盘")
    parser.add_argument("--no-elevate", action="store_true", help="未提权时直接失败（供登录计划任务使用）")
    parser.add_argument("--elevated", action="store_true", help=argparse.SUPPRESS)
    parser.add_argument("--log", type=str, help=argparse.SUPPRESS)
    parser.add_argument("--apply-stage", type=str, help=argparse.SUPPRESS)
    args = parser.parse_args()

    set_log_file(Path(args.log) if args.log else None)

    if args.apply_stage:
        return apply_stage(Path(args.apply_stage))

    package = find_package()
    if not package:
        log(f"[-] 未找到已安装的 {PACKAGE_NAME} 包，请先在 Microsoft Store 安装 Codex / ChatGPT 桌面版。")
        return 4
    package_dir = Path(package["InstallLocation"])
    log(f"[*] 已定位包: {package['PackageFullName']}")

    if args.restore:
        if args.dry_run:
            return describe_restore(package)
        if not is_admin():
            if args.no_elevate:
                log("[-] 还原需要管理员权限，请以管理员身份运行。")
                return 3
            forward = ["--restore"] + (["--kill"] if args.kill else [])
            exit_code = elevate(forward)
        else:
            exit_code = restore_stage(package, kill=args.kill)
        if exit_code == 0:
            finalize_parent(package, relaunch=bool(args.kill or args.relaunch), restart=args.restart_explorer)
        return exit_code

    style_name, master_path = load_master(args.style, args.custom_icon)
    assets = collect_assets(package_dir)
    stage_dir = Path(tempfile.gettempdir()) / "taskbarguard-codex-stage" / package["PackageFullName"]
    if stage_dir.exists():
        shutil.rmtree(stage_dir, ignore_errors=True)
    staged = stage_images(open_master(master_path), assets, stage_dir)

    matched, pending = plan_targets(package_dir, staged)
    log(f"[*] 款式: {style_name}（母版: {master_path.name}）")
    log(f"[*] 目标资产: {len(staged)} 个，已匹配 {matched} 个，待写入 {pending} 个")
    if args.dry_run:
        for item in staged:
            target = package_dir / "assets" / item["name"]
            mark = "已匹配" if target.is_file() and sha256(target) == item["sha256"] else "待写入"
            log(f"    - assets/{item['name']} ({item['size']}px, {mark})")
        return 0

    if pending == 0 and matched == len(staged):
        log("[+] 当前资产已是指定款式，无需写入（跳过 UAC）。")
        return 0

    manifest = {
        "package": package["PackageFullName"],
        "style": style_name,
        "kill": bool(args.kill),
        "files": staged,
    }
    manifest_path = stage_dir / "manifest.json"
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    if not is_admin():
        if args.no_elevate:
            log("[-] 首次写入需要管理员权限（目标位于 WindowsApps），请以管理员身份运行。")
            return 3
        log("[*] 目标位于 WindowsApps，需要一次 UAC 提权...")
        exit_code = elevate(["--apply-stage", str(manifest_path)])
    else:
        exit_code = apply_stage(manifest_path)

    if exit_code == 0:
        finalize_parent(package, relaunch=bool(args.kill or args.relaunch), restart=args.restart_explorer)
        shutil.rmtree(stage_dir, ignore_errors=True)
    return exit_code


if __name__ == "__main__":
    try:
        exit_code = main()
    except Exception:
        import traceback

        if _LOG_HANDLE is not None:
            traceback.print_exc(file=_LOG_HANDLE)
            _LOG_HANDLE.flush()
        traceback.print_exc()
        exit_code = 1
    sys.exit(exit_code)
