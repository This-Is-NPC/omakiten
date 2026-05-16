#!/usr/bin/env python3
"""One-shot migration: rewrite `description:` literals in notification
YAMLs into `${{intl:notifications.<slug>.description}}` tokens and
emit the matching en-catalog entries to stdout.

Reads / writes defaults/notifications/*.yaml in place. Idempotent:
descriptions whose value is already a `${{intl:...}}` token are
skipped.
"""

from __future__ import annotations

import re
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
NOTIF_DIR = REPO / "defaults" / "notifications"
PATTERN = re.compile(r"^(description:\s*)(.+?)\s*$")
TOKEN_PATTERN = re.compile(r"^\$\{\{intl:[^}]+\}\}$")


def migrate(path: Path) -> tuple[str, str] | None:
    slug = path.stem
    lines = path.read_text().splitlines(keepends=True)
    out: list[str] = []
    captured: tuple[str, str] | None = None
    for line in lines:
        match = PATTERN.match(line)
        if not match or captured is not None:
            out.append(line)
            continue
        prefix, value = match.group(1), match.group(2).strip()
        if value.startswith('"') and value.endswith('"'):
            value = value[1:-1]
        if TOKEN_PATTERN.match(value):
            out.append(line)
            continue
        key = f"notifications.{slug}.description"
        ending = "\n" if line.endswith("\n") else ""
        out.append(f'{prefix}"${{{{intl:{key}}}}}"{ending}')
        captured = (key, value)
    if captured is None:
        return None
    path.write_text("".join(out))
    return captured


def main() -> None:
    entries: dict[str, str] = {}
    for path in sorted(NOTIF_DIR.glob("*.yaml")):
        captured = migrate(path)
        if captured is None:
            continue
        key, value = captured
        entries[key] = value
    for key in sorted(entries):
        value = entries[key].replace('"', '\\"')
        print(f'  {key}: "{value}"')


if __name__ == "__main__":
    main()
