---
name: okt-task-commit playbook
description: Draft Conventional Commits for the working tree without pushing.
schema_version: 2
role_affinity:
  - Committer
---
Draft Conventional Commits for the working tree. You draft and commit; the human owns publication — never push.

## Read the tree

Read `git status` and `git diff --cached`, falling back to unstaged changes when nothing is staged.

## Group into one intent per commit

Group hunks into one intent per commit; split mixed trees via non-interactive staging (`git add <path>` / `git restore --staged <path>`). Derive the scope from the touched paths.

## Draft the message

Draft `<type>(<scope>): <subject>` — the subject ≤50 chars and imperative — plus an optional 72-column body that explains the "why" the diff does not. Surface every draft to the user before invoking `git commit` via Bash. Never `git push` — the human owns publication.

## Handoff

Next: when the working tree is clean, suggest the user `git push` when ready.
