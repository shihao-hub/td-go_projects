from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def git(*args: str) -> str:
    result = subprocess.run(
        ["git", *args], cwd=ROOT, capture_output=True, text=True, check=False
    )
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or f"git {' '.join(args)} failed")
    return result.stdout.strip()


def build(mode: str) -> Path:
    commit = git("rev-parse", "HEAD")
    dirty = bool(git("status", "--porcelain"))
    exact_tag = ""
    try:
        exact_tag = git("describe", "--tags", "--exact-match", "HEAD")
    except RuntimeError:
        pass

    if mode == "release":
        if dirty:
            raise RuntimeError("Release 构建要求工作区干净，但当前存在未提交修改")
        if not exact_tag:
            raise RuntimeError("Release 构建要求当前 HEAD 对齐 git tag")
        version = exact_tag.removeprefix("v")
        output_name = "taskbarguard.exe"
    else:
        suffix = "-dirty" if dirty else "-dev"
        version = f"{exact_tag.removeprefix('v') if exact_tag else 'dev'}{suffix}"
        output_name = "taskbarguard-dev.exe"

    output = ROOT / output_name
    ldflags = f"-s -w -X main.version={version} -X main.commitHash={commit}"
    print(f"[build] {mode}: {output_name} (version={version}, commit={commit[:12]})")
    subprocess.run(
        ["go", "build", "-trimpath", "-ldflags", ldflags, "-o", str(output), "."],
        cwd=ROOT,
        check=True,
    )
    return output


def main() -> int:
    parser = argparse.ArgumentParser(description="TaskbarGuard 双轨构建脚本")
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--dev", action="store_true", help="构建开发版")
    group.add_argument("--release", action="store_true", help="构建正式版")
    args = parser.parse_args()
    try:
        if args.release:
            build("release")
        elif args.dev:
            build("dev")
        else:
            build("dev")
            build("release")
    except (OSError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"[build] 错误: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
