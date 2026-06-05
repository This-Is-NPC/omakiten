---
name: okt-note-free playbook
description: Capture a free-form knowledge note (project or global) without ceremony.
schema_version: 2
role_affinity:
  - Scribe
---
Capture a free-form knowledge note without ceremony. The note is persisted through the scope-aware `comments.*` surface.

## Resolve scope and kind

Resolve scope from `--scope` (default `project` when the cwd resolves; explicit `--scope global` always wins). Resolve kind from `--kind` (default `free`); reject `handoff`, `standup-digest`, and `recap` here — those belong to their dedicated commands. Take the title from `--title` and the body from the prompt or stdin.

## Persist via comments

Call `templates.show note-free` to fetch the minimal scaffold, then persist via `comments.add` with the resolved scope (`project`, or `universal` for `--scope global`) and no `task_id`. Reject an empty body or empty title; when the cwd is ambiguous (multiple projects resolve) require `--project <slug>`.

## Handoff

Next: suggest `okt-note-list` to confirm the note landed, or `okt-note-recap` to see it folded into the project timeline.
