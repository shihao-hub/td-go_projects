# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
# build.py：typeai 单进程终端 AI 对话工具的 Go 语言构建入口。
# 用法:
#   python build.py                           # 默认构建（注入 Version=dev，产物输出到 build/）
#   python build.py -v v1.0.0                 # 指定发布版本号（注入到 cli.Version）
#   python build.py --clean -v v1.0.0         # 构建前先清理 build/ 输出目录
#   python build.py -o dist -v v1.0.0         # 自定义二进制输出目录
#   python build.py --help                    # 查看全部命令行参数选项
# 说明:
#   1. 纯标准库实现：零第三方 Python 依赖，遵循 PEP 723 内联元数据规范，兼容 Python 3.10+。
#   2. 跨平台产物：Windows 生成 build/typeai.exe，Linux / macOS 生成 build/typeai。
#   3. 产物目录管理：build/ 目录为构建输出物，已列入 .gitignore，随时可清理或重建。
#   4. 本文件必须保持 UTF-8 无 BOM 编码。
# 编译参数原理:
#   -trimpath:
#       抹除二进制中编译机器的绝对源码路径，保证构建产物的路径无关性与环境安全性。
#   -ldflags "-s -w -X typeai/internal/cli.Version=<version>":
#       -s: 忽略符号表信息（symbol table）。
#       -w: 忽略 DWARF 调试信息（debug information），与 -s 配合可大幅削减二进制体积约 30%-40%。
#       -X: 在链接期将版本字符串静态注入到 `typeai/internal/cli.Version` 变量，
#           使得二进制运行时执行 `typeai version` 即可准确返回对应的构建版本。
# 构建后验收:
#   .\build\typeai.exe version                # 验证注入的版本号
#   .\build\typeai.exe schema                 # 导出 CLI 契约协议（纯内存零 I/O）
#   .\build\typeai.exe help                   # 查看命令行帮助

from __future__ import annotations

import argparse
from pathlib import Path
import shutil
import subprocess
import sys


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Build the typeai binary.",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument(
        "--version", "-v",
        default="dev",
        help="Version string to embed in the binary (cli.Version).",
    )
    parser.add_argument(
        "--output-dir", "-o",
        type=Path,
        default=Path("build"),
        help="Directory to place the built binary.",
    )
    parser.add_argument(
        "--clean",
        action="store_true",
        help="Clean the output directory before building.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()

    project_root = Path(__file__).resolve().parent
    output_dir = (project_root / args.output_dir).resolve()

    if args.clean and output_dir.exists():
        print(f"Cleaning {output_dir}...")
        shutil.rmtree(output_dir)

    output_dir.mkdir(parents=True, exist_ok=True)

    exe_name = "typeai.exe" if sys.platform == "win32" else "typeai"
    output_path = output_dir / exe_name

    ldflags = f"-s -w -X typeai/internal/cli.Version={args.version}"
    cmd = [
        "go",
        "build",
        "-trimpath",
        "-ldflags",
        ldflags,
        "-o",
        str(output_path),
        "./cmd/typeai",
    ]

    print(f"==> Building typeai (version={args.version})...")
    print(f"    CMD: {' '.join(cmd)}")

    try:
        proc = subprocess.run(cmd, cwd=project_root, check=False)
        if proc.returncode != 0:
            print(f"Build failed with exit code {proc.returncode}", file=sys.stderr)
            return proc.returncode
    except FileNotFoundError:
        print(
            "Error: 'go' command not found. Please ensure Go is installed and in your PATH.",
            file=sys.stderr,
        )
        return 1

    print(f"Built successfully: {output_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
