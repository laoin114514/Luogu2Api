#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""生成 pkg/fakeuseragent/data/browsers.jsonl.gz —— Go 包内嵌的 User-Agent 数据。

数据链路（每一环都可追溯）：

    Intoli LLC 的 user-agents 数据
        -> fake-useragent 的 ua-converter/ua_convert.py（ua-parser 解析 + 字段重映射）
        -> fake-useragent/src/fake_useragent/data/browsers.jsonl   ← 本脚本的输入
        -> 逐行校验 + gzip
        -> pkg/fakeuseragent/data/browsers.jsonl.gz                ← 本脚本的输出

刻意"零加工"：既不去重也不改字段。上游同一个 UA 字符串会重复出现多次
（重复最多的那条出现 1502 次），Python 版用 random.choice 在"行"上等概率取样，
重复行因此承担了加权作用；去重会改变随机分布，破坏两个版本的一致性。

用法：

    python scripts/gen_ua_data.py                          # 从上游地址下载后生成
    python scripts/gen_ua_data.py --input browsers.jsonl   # 用本地文件生成
    python scripts/gen_ua_data.py --check                  # 只校验现有产物，不写文件

gzip 的压缩级别与 mtime 固定，保证同样输入必得同样输出，--check 才能做字节级比对。
"""

from __future__ import annotations

import argparse
import gzip
import io
import json
import sys
import urllib.request
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
OUTPUT = REPO_ROOT / "pkg" / "fakeuseragent" / "data" / "browsers.jsonl.gz"

DEFAULT_URL = (
    "https://raw.githubusercontent.com/fake-useragent/fake-useragent/"
    "main/src/fake_useragent/data/browsers.jsonl"
)

REQUIRED_FIELDS = (
    "useragent",
    "percent",
    "type",
    "device_brand",
    "browser",
    "browser_version",
    "browser_version_major_minor",
    "os",
    "os_version",
    "platform",
)

VALID_TYPES = {"desktop", "mobile", "tablet"}


def fail(msg: str) -> "None":
    print(f"错误: {msg}", file=sys.stderr)
    raise SystemExit(1)


def fetch(url: str) -> bytes:
    print(f"下载 {url}")
    try:
        with urllib.request.urlopen(url, timeout=60) as resp:
            return resp.read()
    except OSError as exc:
        fail(f"下载失败: {exc}\n（也可以先用 --input 指定本地 browsers.jsonl）")


def validate(raw: bytes) -> int:
    """逐行校验 JSONL：畸形数据必须在生成阶段失败，而不是留给 Go 侧运行时才发现。"""
    text = raw.decode("utf-8")
    if not text.endswith("\n"):
        fail("输入最后一行没有换行符，JSON Lines 要求每行以 \\n 结尾")

    count = 0
    for lineno, line in enumerate(text.splitlines(), start=1):
        if not line.strip():
            fail(f"第 {lineno} 行为空行")
        try:
            item = json.loads(line)
        except json.JSONDecodeError as exc:
            fail(f"第 {lineno} 行不是合法 JSON: {exc}")
        if not isinstance(item, dict):
            fail(f"第 {lineno} 行不是 JSON 对象")
        missing = [k for k in REQUIRED_FIELDS if k not in item]
        if missing:
            fail(f"第 {lineno} 行缺少字段: {', '.join(missing)}")
        if not item["useragent"]:
            fail(f"第 {lineno} 行的 useragent 为空")
        if item["type"] not in VALID_TYPES:
            fail(f"第 {lineno} 行的 type 非法: {item['type']!r}")
        for field in ("percent", "browser_version_major_minor"):
            value = item[field]
            if isinstance(value, bool) or not isinstance(value, (int, float)):
                fail(f"第 {lineno} 行的 {field} 不是数字: {value!r}")
        count += 1

    if count < 1000:
        fail(f"只解析出 {count} 条记录，数据疑似不完整")
    return count


def compress(raw: bytes) -> bytes:
    buf = io.BytesIO()
    # mtime=0：gzip 头不写时间戳，产出才可复现
    with gzip.GzipFile(fileobj=buf, mode="wb", compresslevel=9, mtime=0) as fh:
        fh.write(raw)
    return buf.getvalue()


def main() -> int:
    parser = argparse.ArgumentParser(
        description="生成 Go 包内嵌的 UA 数据 (pkg/fakeuseragent/data/browsers.jsonl.gz)"
    )
    src = parser.add_mutually_exclusive_group()
    src.add_argument("--input", type=Path, help="本地 browsers.jsonl 路径")
    src.add_argument("--url", help=f"上游地址（默认 {DEFAULT_URL}）")
    parser.add_argument("--check", action="store_true", help="只校验产物与输入是否一致，不写文件")
    args = parser.parse_args()

    if args.input:
        print(f"读取 {args.input}")
        raw = args.input.read_bytes()
    else:
        raw = fetch(args.url or DEFAULT_URL)

    count = validate(raw)
    payload = compress(raw)

    if args.check:
        current = OUTPUT.read_bytes() if OUTPUT.exists() else b""
        if current == payload:
            print(f"OK: {OUTPUT.relative_to(REPO_ROOT)} 与输入一致（{count} 条）")
            return 0
        fail(f"{OUTPUT.relative_to(REPO_ROOT)} 与输入不一致，需要重新生成")

    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_bytes(payload)
    print(
        f"写入 {OUTPUT.relative_to(REPO_ROOT)}：{count} 条，"
        f"原始 {len(raw)} 字节 -> 压缩后 {len(payload)} 字节"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
