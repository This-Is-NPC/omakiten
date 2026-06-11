---
name: okt-task-review playbook
description: Walk the diff through Fowler/Beck/Martin/Feathers lens and surface findings + refactor opportunities.
schema_version: 2
role_affinity:
  - Reviewer
---
Walk the diff with the loaded lens. This is a read-only review — you surface findings, you never edit files or commit.

## Read every hunk first

Run `git diff <base>..HEAD` (default base `main`; use staged when explicit) and read every hunk before writing findings.

## Order the pass and cite methodology

Order the pass correctness → security → smells → refactor opportunities → scalability/performance. Cite methodology by name when applicable — `Extract Function — Fowler`, `Feature Envy — Fowler/Beck`, `Sprout Method — Feathers`, `OCP — Martin`. Tag every finding by severity (`error` / `warning` / `info`).

## Post the filled scaffolds

Call `templates.show comment-review-findings` and `templates.show comment-refactor-opportunities` for the scaffolds, then persist each filled task comment with `comments.add` (`scope=task`, the reviewed `task_id`, `author_type=agent`). Read-only means no file edits and no `git commit`; writing review comments is the durable artifact.

## Handoff

Next: when findings need fixes, suggest `okt-task-implement` with the finding ids.
