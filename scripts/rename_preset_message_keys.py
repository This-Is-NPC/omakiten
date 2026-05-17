#!/usr/bin/env python3
"""One-shot rename of preset hook intl tokens from positional 000-NNN
indices to descriptive event names. Re-run safe (idempotent): the
replacement loop only triggers when the legacy NNN form is still present.

After this lands, future preset hook additions follow the descriptive
convention directly — no positional indices."""

from __future__ import annotations

import pathlib
import re

REPO_ROOT = pathlib.Path(__file__).resolve().parents[1]
CONFIG_DIR = REPO_ROOT / "defaults" / "config"

COMMON_28 = [
    "task_created",
    "task_moved",
    "task_edited",
    "task_completed",
    "guard_task_delete",
    "guard_comment_delete",
    "guard_task_transition",
    "guard_task_edit",
    "guard_comment_edit",
    "guard_task_archive",
    "guard_task_unarchive",
    "task_removed",
    "comment_removed",
    "task_archived",
    "task_unarchived",
    "comment_added_human",
    "comment_added_agent",
    "comment_edited",
    "tag_added",
    "tag_removed",
    "dependency_added",
    "dependency_removed",
    "error_recorded",
    "error_searched",
    "solution_added",
    "solution_liked",
    "solution_failed",
    "solution_viewed_top",
]

OMAKASE_29 = (
    COMMON_28[:11]
    + ["bundle_swapped_orphans"]
    + COMMON_28[11:]
)

MAPPINGS = {
    "izakaya": COMMON_28,
    "kaiseki": COMMON_28,
    "shokunin": COMMON_28,
    "omakase": OMAKASE_29,
}


def main() -> int:
    for preset, names in MAPPINGS.items():
        path = CONFIG_DIR / f"{preset}.yaml"
        original = path.read_text(encoding="utf-8")
        text = original
        for i, name in enumerate(names):
            old = f"${{{{intl:notifications.{preset}.{i:03d}.message}}}}"
            new = f"${{{{intl:notifications.{preset}.{name}.message}}}}"
            text = text.replace(old, new)
        leftover = re.findall(
            rf"\$\{{\{{intl:notifications\.{preset}\.\d{{3}}\.message\}}\}}",
            text,
        )
        if leftover:
            raise SystemExit(f"{preset}: leftover legacy tokens {leftover[:3]}")
        if text != original:
            path.write_text(text, encoding="utf-8")
            print(f"updated {path.relative_to(REPO_ROOT)}")
        else:
            print(f"unchanged {path.relative_to(REPO_ROOT)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
