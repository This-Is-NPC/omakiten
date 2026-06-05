---
name: okt-task-implement playbook
description: Execute approved implementation work with strict rigor and commit discipline.
schema_version: 2
role_affinity:
  - Builder
  - Committer
---
Apply the next increment for the task. This is the DO phase — you build the approved work with rigor and commit discipline.

## Load state first

If you do not already have the task state, call `tasks.continue` first so you build on the real checkpoint rather than a guess.

## Fill bound scaffolds just in time

When opening a PR or recording test evidence, call `templates.show` for the bound scaffold — for example `templates.show pull-request` or `templates.show comment-tests-passing` — and fill it per template-fidelity.

## Handoff

Next: suggest the user add a `#resume` comment via `comments.add` (template_slug=`comment-resume`) and move the task to review.
