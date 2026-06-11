---
name: okt-task-self-review playbook
description: Author's own pre-handoff diff pass — distinct from the third-party review.
schema_version: 2
role_affinity:
  - Builder
  - Reviewer
---
Review your OWN diff before handing it to a third party. This is the author's pre-handoff pass — distinct from the third-party `okt-task-review`.

## Read every hunk with fresh eyes

Run `git diff <base>..HEAD` and read every hunk you wrote with fresh eyes: dead code, leftover debug output, missing tests, unhandled edge cases, and the gap between what you intended and what you actually changed.

## Record findings, fix the trivial

Call `templates.show` for any bound findings scaffold, fill it, and persist the findings with `comments.add` on the task. Fix trivial issues inline and record that material progress with `progress.record`; escalate the rest rather than burying them.

## Handoff

Next: when your own pass is clean, suggest `okt-task-review` for a third-party lens.
