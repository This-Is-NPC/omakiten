---
name: okt-note-recap playbook
description: Recap timeline of recent notes for the active or explicitly selected project.
schema_version: 2
role_affinity:
  - Scribe
  - Concierge
---
Render a recap timeline of recent activity, scoped by the window argument and the active or explicitly selected project. The recap reads through `comments.*`; it is the artifact and is never persisted.

## Single-project window

With a single-project window (the default — project from `--project <slug>` or cwd) it groups recent notes chronologically: resolve the window from `[janela]` / `--since` (default `7d`) and the kind filter from `--kinds <comma-list>` (default all), call `comments.list` with `scope=project` for the project filtered by `since` (the window) and `kind`, then `templates.show note-recap` to fetch the scaffold. Group entries by kind and order them chronologically with a timestamp prefix per bullet.

## Wide window stays project-scoped

With a wide window (`okt-note-recap day` or a large `--since`), stay scoped to the active project unless the invocation args include an explicit `project` slug. Call `comments.list` with `scope=project`, the chosen `since`, and the selected kind filters, then `templates.show note-recap` and fill one project-scoped timeline. Do not promise a cross-project digest unless a future MCP surface can enumerate projects.

## Read-only, always

Read-only either way — never persist; the recap is the artifact. When zero notes/handoffs match the window, surface "nothing in window" (or "no handoffs — run okt-pause" for the cross-project case).

## Handoff

Next: suggest `okt-task-continue` with a specific task id when the recap reveals an open thread to resume.
