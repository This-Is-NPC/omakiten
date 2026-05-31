#!/usr/bin/env python3
"""Apply i18n #184 pass-2 translations (follow-up comment keys) to language packs."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
LANG_DIR = REPO / "defaults" / "languages"
DATA_FILE = Path(__file__).with_name("i18n_184_pass2_translations.json")


def yaml_quote(value: str) -> str:
    escaped = value.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def format_key_block(key: str, value: str) -> str:
    if "\n" in value:
        body = "\n".join(f"    {line}" for line in value.split("\n"))
        return f"  {key}: |-\n{body}\n"
    return f"  {key}: {yaml_quote(value)}"


def replace_key(content: str, key: str, value: str) -> str:
    new_block = format_key_block(key, value)
    quoted = rf'  {re.escape(key)}: "(?:[^"\\]|\\.)*"'
    # Literal blocks may contain blank lines (no indent) between paragraphs.
    block = rf'  {re.escape(key)}: \|-\n(?:(?:    .*)?\n)+'
    pattern = re.compile(f"(?:{quoted}|{block})")
    match = pattern.search(content)
    if not match:
        raise KeyError(f"key not found in file: {key}")
    return content[: match.start()] + new_block + content[match.end() :]


def apply_locale(code: str, translations: dict[str, str]) -> None:
    path = LANG_DIR / f"{code}.yaml"
    content = path.read_text(encoding="utf-8")
    for key, value in translations.items():
        content = replace_key(content, key, value)
    path.write_text(content, encoding="utf-8")


def main() -> int:
    if not DATA_FILE.exists():
        print(f"error: {DATA_FILE} not found", file=sys.stderr)
        return 1
    data: dict[str, dict[str, str]] = json.loads(DATA_FILE.read_text(encoding="utf-8"))
    for code, translations in sorted(data.items()):
        if code == "en":
            continue
        print(f"applying {code} ({len(translations)} keys)...")
        apply_locale(code, translations)
    print("done")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
