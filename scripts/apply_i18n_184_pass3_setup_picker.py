#!/usr/bin/env python3
"""Apply i18n #184 pass 3 — cli.setup.picker.step + hint.back (comment #7635)."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
LANG_DIR = REPO / "defaults" / "languages"
DATA_FILE = Path(__file__).with_name("i18n_184_pass3_setup_picker.json")

KEYS = ["cli.setup.picker.step", "cli.setup.picker.hint.back"]
ANCHOR = "cli.setup.picker.hint.input"


def yaml_quote(value: str) -> str:
    escaped = value.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def format_key_line(key: str, value: str) -> str:
    return f"  {key}: {yaml_quote(value)}"


def insert_after_anchor(content: str, key: str, value: str) -> str:
    line = format_key_line(key, value)
    quoted = rf'  {re.escape(key)}: "(?:[^"\\]|\\.)*"'
    if re.search(quoted, content):
        # Replace existing value.
        return re.sub(quoted, line.strip(), content, count=1)
    anchor = rf'(  {re.escape(ANCHOR)}: "(?:[^"\\]|\\.)*"\n)'
    match = re.search(anchor, content)
    if not match:
        raise KeyError(f"anchor not found: {ANCHOR}")
    insert_at = match.end()
    return content[:insert_at] + line + "\n" + content[insert_at:]


def apply_locale(code: str, translations: dict[str, str]) -> None:
    path = LANG_DIR / f"{code}.yaml"
    if not path.exists():
        raise FileNotFoundError(path)
    content = path.read_text(encoding="utf-8")
    for key in KEYS:
        if key not in translations:
            raise KeyError(f"missing translation for {code}: {key}")
        content = insert_after_anchor(content, key, translations[key])
    path.write_text(content, encoding="utf-8")


def main() -> int:
    if not DATA_FILE.exists():
        print(f"error: {DATA_FILE} not found", file=sys.stderr)
        return 1
    data: dict[str, dict[str, str]] = json.loads(DATA_FILE.read_text(encoding="utf-8"))
    for code, translations in sorted(data.items()):
        if code == "en":
            continue
        print(f"applying {code}...")
        apply_locale(code, translations)
    print("done")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
