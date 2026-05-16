#!/usr/bin/env python3
"""One-shot migration: rewrite hook `message:` literals in preset YAMLs
into `${{intl:notifications.<preset>.<seq>.message}}` tokens and emit
the matching en-catalog entries to stdout.

Usage: scripts/migrate_preset_messages.py
Reads / writes defaults/config/{omakase,izakaya,kaiseki,shokunin}.yaml
in place and prints the keys block for defaults/languages/en.yaml.

Idempotent: lines whose message value is already a `${{intl:...}}`
token are skipped (no double-rewrap, no duplicate key emission).
"""

from __future__ import annotations

import re
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
PRESETS = ["omakase", "izakaya", "kaiseki", "shokunin"]
PATTERN = re.compile(r'^(\s*message:\s*)"(.+)"\s*$')
TOKEN_PATTERN = re.compile(r'^\$\{\{intl:[^}]+\}\}$')


def migrate(preset: str) -> dict[str, str]:
    path = REPO / "defaults" / "config" / f"{preset}.yaml"
    lines = path.read_text().splitlines(keepends=True)
    keys: dict[str, str] = {}
    seq = 0
    out: list[str] = []
    for line in lines:
        match = PATTERN.match(line)
        if not match:
            out.append(line)
            continue
        prefix, value = match.group(1), match.group(2)
        if TOKEN_PATTERN.match(value):
            out.append(line)
            continue
        key = f"notifications.{preset}.{seq:03d}.message"
        keys[key] = value
        ending = "\n" if line.endswith("\n") else ""
        out.append(f'{prefix}"${{{{intl:{key}}}}}"{ending}')
        seq += 1
    path.write_text("".join(out))
    return keys


def main() -> None:
    all_keys: dict[str, str] = {}
    for preset in PRESETS:
        all_keys.update(migrate(preset))
    for key in sorted(all_keys):
        value = all_keys[key].replace('"', '\\"')
        print(f'  {key}: "{value}"')


if __name__ == "__main__":
    main()
