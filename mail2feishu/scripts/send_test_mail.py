# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""mail2feishu 链路自测工具（开发/运维用，不参与构建）。

QQ 邮箱的 SMTP 与 IMAP 共用同一份授权码，因此凭证直接从 mail2feishu 的
config.yaml 读取，脚本本身不保存任何密钥，也不会把口令打印到终端。

为什么需要 --deliver：QQ 会把「自己发给自己」的邮件分流到自定义文件夹
（例如「其他文件夹/测试目录」），而 mail2feishu 只监听 INBOX，
所以自发邮件测不到推送。--deliver 把已投递的邮件 IMAP COPY 进 INBOX，
等价于「一封新邮件到达收件箱」，可以打通完整推送链路。

用法：
  uv run scripts/send_test_mail.py                  # SMTP 给自己发一封自测邮件
  uv run scripts/send_test_mail.py --verify         # 列出 INBOX 最新 5 封
  uv run scripts/send_test_mail.py --find KEYWORD   # 所有文件夹按主题查找
  uv run scripts/send_test_mail.py --deliver KEYWORD    # 复制到 INBOX
  uv run scripts/send_test_mail.py --cleanup KEYWORD    # 清理预演
  uv run scripts/send_test_mail.py --cleanup KEYWORD --apply  # 真正删除
"""

from __future__ import annotations

import argparse
import base64
import imaplib
import smtplib
import ssl
import sys
from email import message_from_bytes
from email.header import decode_header, make_header
from email.message import EmailMessage
from email.utils import formatdate, make_msgid
from pathlib import Path

import yaml

PROJECT_NAME = "mail2feishu"
SMTP_HOST = "smtp.qq.com"
SMTP_PORT = 465
IMAP_HOST = "imap.qq.com"
IMAP_PORT = 993
DEFAULT_SUBJECT = "m2f-test 邮件链路自测"
DEFAULT_BODY = "这是一封 mail2feishu 链路自测邮件，可以忽略或删除。"
SCAN_LIMIT = 200
TRASH_HINTS = ("已删除", "deleted", "trash")


def setup_console() -> None:
    for stream in (sys.stdout, sys.stderr):
        if stream.encoding and stream.encoding.lower() not in ("utf-8", "utf8"):
            stream.reconfigure(encoding="utf-8", errors="replace")


def default_config() -> Path:
    """按 dev → prod 顺序探测默认配置路径。"""
    base = Path.home() / "AppData/Roaming" if sys.platform == "win32" else Path.home() / ".config"
    for mode in ("dev", "prod"):
        candidate = base / "language_projects" / PROJECT_NAME / mode / "config.yaml"
        if candidate.is_file():
            return candidate
    return base / "language_projects" / PROJECT_NAME / "dev" / "config.yaml"


def load_credentials(path: Path) -> tuple[str, str]:
    if not path.is_file():
        raise SystemExit(f"配置文件不存在: {path}")
    data = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    imap = data.get("imap") or {}
    username = str(imap.get("username") or "").strip()
    password = str(imap.get("password") or "")
    if not username or not password:
        raise SystemExit(f"配置缺少 imap.username / imap.password: {path}")
    return username, password


def decode(value: str | None) -> str:
    if not value:
        return ""
    try:
        return str(make_header(decode_header(value)))
    except Exception:
        return value


def decode_folder(name: str) -> str:
    """把 IMAP 的 modified UTF-7 文件夹名还原成可读文本。"""
    out: list[str] = []
    i = 0
    while i < len(name):
        if name[i] != "&":
            out.append(name[i])
            i += 1
            continue
        end = name.find("-", i)
        if end == -1:
            out.append(name[i:])
            break
        chunk = name[i + 1 : end]
        if not chunk:
            out.append("&")
        else:
            b64 = chunk.replace(",", "/")
            padding = (-len(b64)) % 4
            try:
                out.append(base64.b64decode(b64 + "=" * padding).decode("utf-16-be"))
            except Exception:
                out.append(name[i : end + 1])
        i = end + 1
    return "".join(out)


def list_folders(imap: imaplib.IMAP4_SSL) -> list[str]:
    status, data = imap.list()
    if status != "OK":
        return []
    names: list[str] = []
    for line in data or []:
        if not line:
            continue
        raw = line.decode("utf-8", "replace") if isinstance(line, bytes) else line
        parts = raw.rsplit(" ", 1)
        if len(parts) == 2:
            names.append(parts[1].strip('"'))
    return names


def connect_imap(username: str, password: str) -> imaplib.IMAP4_SSL:
    return imaplib.IMAP4_SSL(IMAP_HOST, IMAP_PORT, ssl_context=ssl.create_default_context())


def logout_quietly(imap: imaplib.IMAP4_SSL) -> None:
    try:
        imap.logout()
    except Exception:
        pass


def send(username: str, password: str, subject: str, body: str) -> int:
    msg = EmailMessage()
    msg["From"] = username
    msg["To"] = username
    msg["Subject"] = subject
    msg["Date"] = formatdate(localtime=True)
    msg["Message-ID"] = make_msgid(domain="qq.com")
    msg.set_content(body)

    with smtplib.SMTP_SSL(
        SMTP_HOST, SMTP_PORT, timeout=30, context=ssl.create_default_context()
    ) as smtp:
        smtp.login(username, password)
        smtp.send_message(msg)
    print(f"已发送: {username} -> {username}")
    print(f"主题: {subject}")
    print("提示: QQ 可能把它投到自定义文件夹而不是 INBOX，用 --find 定位、--deliver 注入。")
    return 0


def verify(username: str, password: str, limit: int) -> int:
    imap = connect_imap(username, password)
    try:
        imap.login(username, password)
        imap.select("INBOX", readonly=True)
        status, data = imap.uid("search", None, "ALL")
        if status != "OK":
            print(f"检索失败: {status}", file=sys.stderr)
            return 1
        uids = (data[0] or b"").split()[-limit:]
        print(f"INBOX 最新 {len(uids)} 封（readonly，不改动邮件状态）:")
        for uid in uids:
            status, fetched = imap.uid(
                "fetch", uid, "(BODY.PEEK[HEADER.FIELDS (SUBJECT FROM DATE)])"
            )
            if status != "OK" or not fetched:
                continue
            raw = fetched[0][1] if isinstance(fetched[0], tuple) else fetched[0]
            msg = message_from_bytes(raw)
            print(
                f"  UID {uid.decode()}"
                f" | {decode(msg.get('From'))}"
                f" | {decode(msg.get('Subject'))}"
                f" | {msg.get('Date', '').strip()}"
            )
    finally:
        logout_quietly(imap)
    return 0


def find(username: str, password: str, keyword: str) -> int:
    imap = connect_imap(username, password)
    hits = 0
    try:
        imap.login(username, password)
        for folder in list_folders(imap):
            status, _ = imap.select(f'"{folder}"', readonly=True)
            if status != "OK":
                continue
            status, data = imap.uid("search", None, "ALL")
            if status != "OK" or not data or not data[0]:
                continue
            for uid in data[0].split()[-SCAN_LIMIT:]:
                status, fetched = imap.uid(
                    "fetch", uid, "(BODY.PEEK[HEADER.FIELDS (SUBJECT DATE)])"
                )
                if status != "OK" or not fetched:
                    continue
                raw = fetched[0][1] if isinstance(fetched[0], tuple) else fetched[0]
                msg = message_from_bytes(raw)
                subject = decode(msg.get("Subject"))
                if keyword not in subject:
                    continue
                hits += 1
                print(
                    f"  命中: {decode_folder(folder)} UID={uid.decode()}"
                    f" | {subject} | {msg.get('Date', '').strip()}"
                )
    finally:
        logout_quietly(imap)
    if hits == 0:
        print(f"未找到主题含 {keyword!r} 的邮件")
        return 1
    return 0


def deliver(username: str, password: str, keyword: str) -> int:
    """把匹配邮件复制到 INBOX，模拟「新邮件到达收件箱」。只 COPY，不删除原件。"""
    imap = connect_imap(username, password)
    moved = 0
    try:
        imap.login(username, password)
        for folder in list_folders(imap):
            if folder.upper() == "INBOX":
                continue
            try:
                status, _ = imap.select(f'"{folder}"', readonly=True)
            except imaplib.IMAP4.error:
                continue
            if status != "OK":
                continue
            status, data = imap.uid("search", None, "ALL")
            if status != "OK" or not data or not data[0]:
                continue
            for uid in data[0].split()[-SCAN_LIMIT:]:
                status, fetched = imap.uid(
                    "fetch", uid, "(BODY.PEEK[HEADER.FIELDS (SUBJECT)])"
                )
                if status != "OK" or not fetched:
                    continue
                raw = fetched[0][1] if isinstance(fetched[0], tuple) else fetched[0]
                if keyword not in decode(message_from_bytes(raw).get("Subject")):
                    continue
                status, _ = imap.uid("copy", uid, "INBOX")
                if status == "OK":
                    moved += 1
                    print(f"  已复制到 INBOX: 源={decode_folder(folder)} UID={uid.decode()}")
    finally:
        logout_quietly(imap)
    if moved == 0:
        print(f"没有可复制的匹配邮件（{keyword!r}）")
        return 1
    print("提示: QQ 的 COPY 可能有几十秒延迟，稍等再看 --verify 或监听日志。")
    return 0


def cleanup(username: str, password: str, keyword: str, apply: bool) -> int:
    """删除主题含 keyword 的自测邮件。默认只预演；逐封校验主题后才动手。"""
    imap = connect_imap(username, password)
    matched: list[tuple[str, bytes, str]] = []
    trash: str | None = None
    try:
        imap.login(username, password)
        folders = list_folders(imap)
        trash = next(
            (
                f
                for f in folders
                if any(hint in decode_folder(f).lower() for hint in TRASH_HINTS)
            ),
            None,
        )
        print(f"垃圾箱: {decode_folder(trash) if trash else '未找到（将永久删除）'}")

        for folder in folders:
            try:
                status, _ = imap.select(f'"{folder}"', readonly=True)
            except imaplib.IMAP4.error:
                continue
            if status != "OK":
                continue
            status, data = imap.uid("search", None, "ALL")
            if status != "OK" or not data or not data[0]:
                continue
            for uid in data[0].split()[-SCAN_LIMIT:]:
                status, fetched = imap.uid(
                    "fetch", uid, "(BODY.PEEK[HEADER.FIELDS (SUBJECT)])"
                )
                if status != "OK" or not fetched:
                    continue
                raw = fetched[0][1] if isinstance(fetched[0], tuple) else fetched[0]
                subject = decode(message_from_bytes(raw).get("Subject"))
                if keyword not in subject:
                    continue
                matched.append((folder, uid, subject))
                print(f"  命中: {decode_folder(folder)} UID={uid.decode()} | {subject}")

        if not matched:
            print("没有匹配邮件，未做任何改动")
            return 0
        if not apply:
            print(f"预演完成：共 {len(matched)} 封，未做任何改动（确认后加 --apply）")
            return 0

        for folder, uid, _ in matched:
            imap.select(f'"{folder}"')
            if trash:
                imap.uid("copy", uid, trash)
            imap.uid("store", uid, "+FLAGS", "(\\Deleted)")
            # 只用 UID EXPUNGE，避免普通 EXPUNGE 顺手清掉其它已标记删除的邮件
            imap.uid("expunge", uid)
        print(f"已处理：{len(matched)} 封 → {'垃圾箱' if trash else '永久删除'}")
    finally:
        logout_quietly(imap)
    return 0


def main() -> int:
    setup_console()
    parser = argparse.ArgumentParser(description="mail2feishu 链路自测")
    parser.add_argument("--config", default=str(default_config()), help="config.yaml 路径")
    parser.add_argument("--subject", default=DEFAULT_SUBJECT, help="自测邮件主题")
    parser.add_argument("--body", default=DEFAULT_BODY, help="自测邮件正文")
    parser.add_argument("--verify", action="store_true", help="列出 INBOX 最新邮件")
    parser.add_argument("--find", metavar="KEYWORD", help="所有文件夹按主题关键词查找")
    parser.add_argument("--deliver", metavar="KEYWORD", help="把匹配邮件复制到 INBOX")
    parser.add_argument("--cleanup", metavar="KEYWORD", help="删除匹配的自测邮件（默认预演）")
    parser.add_argument("--apply", action="store_true", help="与 --cleanup 搭配：真正执行")
    parser.add_argument("--limit", type=int, default=5, help="--verify 显示的条数")
    args = parser.parse_args()

    username, password = load_credentials(Path(args.config))
    if args.cleanup:
        return cleanup(username, password, args.cleanup, args.apply)
    if args.deliver:
        return deliver(username, password, args.deliver)
    if args.find:
        return find(username, password, args.find)
    if args.verify:
        return verify(username, password, args.limit)
    return send(username, password, args.subject, args.body)


if __name__ == "__main__":
    sys.exit(main())
