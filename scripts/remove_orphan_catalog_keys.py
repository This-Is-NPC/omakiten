#!/usr/bin/env python3
"""Remove orphan catalog keys from every bundled language pack."""

from __future__ import annotations

import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
LANG_DIR = REPO / "defaults" / "languages"

# Definitive orphans: zero Go references (see task #184 cleanup).
ORPHAN_KEYS = [
    "cli.err.comment_scope_no_task_id",
    "cli.err.comment_task_scope_requires_id",
    "cli.err.comment_unknown_scope",
    "cli.projects.delete.err.not_found",
    "cli.setup.picker.lang.tui.title",
    "cli.setup.status.complete",
    "cli.setup.warn.harness_index_oor",
    "cli.setup.warn.unknown_harness",
    "notifications.home-project-delete-confirm.cancel_label",
    "tui.empty.sub_tasks",
    "tui.help.task_view.back_board",
    "tui.help.task_view.open_comment",
    "tui.keys.submit_bucket_desc",
    "tui.kicker.sources",
    "tui.kicker.status",
    "tui.log.cli",
    "tui.log.column_header_fmt",
    "tui.log.mcp",
    "tui.log.operation_col",
    "tui.log.project_col",
    "tui.log.total",
    "tui.plans.network.empty_wave",
    "tui.plans.network.wave_header_fmt",
    "tui.plans.status.claim_empty_fmt",
    "tui.plans.status.claim_success_fmt",
    "tui.settings.effective.source.default",
    "tui.settings.effective.source.env",
    "tui.settings.effective.source.project",
    "tui.settings.effective_hint",
]


def remove_key(content: str, key: str) -> str:
    quoted = rf"  {re.escape(key)}: \"(?:[^\"\\]|\\.)*\"\n"
    block = rf"  {re.escape(key)}: \|-\n(?:(?:    .*)?\n)+"
    pattern = re.compile(f"(?:{quoted}|{block})")
    new_content, n = pattern.subn("", content)
    if n == 0:
        raise KeyError(f"key not found: {key}")
    return new_content


def main() -> int:
    paths = sorted(LANG_DIR.glob("*.yaml"))
    for path in paths:
        content = path.read_text(encoding="utf-8")
        for key in ORPHAN_KEYS:
            try:
                content = remove_key(content, key)
            except KeyError:
                print(f"warn: {path.name}: missing {key}", file=sys.stderr)
        path.write_text(content, encoding="utf-8")
        print(f"cleaned {path.name}")
    print("done")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
